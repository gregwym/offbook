import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  AIDeltaPayload,
  AIDonePayload,
  AIErrorPayload,
  AIMessage,
  AIThread,
} from '../types/ai'

export async function listThreads(): Promise<AIThread[]> {
  const res = await apiClient.get<ApiList<AIThread>>('/ai/threads')
  return res.data.data
}

export async function createThread(title?: string): Promise<AIThread> {
  const res = await apiClient.post<ApiItem<AIThread>>('/ai/threads', { title })
  return res.data.data
}

export async function listMessages(threadID: number): Promise<AIMessage[]> {
  const res = await apiClient.get<ApiList<AIMessage>>(`/ai/threads/${threadID}/messages`)
  return res.data.data
}

// --- household-scope variants ------------------------------------------
// Same shapes; routed under /h/* on the backend so authz + context
// builder switch to the household path.

export async function listSharedThreads(): Promise<AIThread[]> {
  const res = await apiClient.get<ApiList<AIThread>>('/h/ai/threads')
  return res.data.data
}

export async function createSharedThread(title?: string): Promise<AIThread> {
  const res = await apiClient.post<ApiItem<AIThread>>('/h/ai/threads', { title })
  return res.data.data
}

export async function listSharedMessages(threadID: number): Promise<AIMessage[]> {
  const res = await apiClient.get<ApiList<AIMessage>>(`/h/ai/threads/${threadID}/messages`)
  return res.data.data
}

// SSEHandlers receives parsed event payloads. The stream caller closes
// after a terminal `done` or `error`; the caller can also abort via the
// AbortController.
export type SSEHandlers = {
  onDelta?: (p: AIDeltaPayload) => void
  onDone?: (p: AIDonePayload) => void
  onError?: (p: AIErrorPayload) => void
}

// streamMessage opens the SSE stream backing the personal AI endpoint.
// We use fetch + ReadableStream (not EventSource) because the backend
// gates the endpoint on the session cookie and EventSource doesn't carry
// credentials by default. The signal lets the page abort if the user
// navigates away mid-stream.
export async function streamMessage(
  threadID: number,
  content: string,
  handlers: SSEHandlers,
  signal?: AbortSignal,
): Promise<void> {
  return streamMessageAt(`/ai/threads/${threadID}/messages`, content, handlers, signal)
}

// streamSharedMessage is the household-scope counterpart. Same wire
// shape, just routed through `/h/ai/...`.
export async function streamSharedMessage(
  threadID: number,
  content: string,
  handlers: SSEHandlers,
  signal?: AbortSignal,
): Promise<void> {
  return streamMessageAt(`/h/ai/threads/${threadID}/messages`, content, handlers, signal)
}

async function streamMessageAt(
  path: string,
  content: string,
  handlers: SSEHandlers,
  signal?: AbortSignal,
): Promise<void> {
  // Reuse the same base URL the rest of the client uses so dev (Vite
  // proxy) and prod (VITE_API_BASE_URL) keep working without conditionals.
  const baseURL = apiClient.defaults.baseURL ?? '/api/v1'
  const res = await fetch(`${baseURL}${path}`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
    body: JSON.stringify({ content }),
    signal,
  })
  if (!res.ok || !res.body) {
    let detail = ''
    try {
      const j = await res.json()
      detail = j?.error ?? ''
    } catch {
      // body wasn't JSON — fall through
    }
    handlers.onError?.({ message: detail || `HTTP ${res.status}` })
    return
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''

  const flushEvent = (raw: string) => {
    // Each event is "event: <type>\ndata: <json>\n\n"
    let evType = ''
    let data = ''
    for (const line of raw.split('\n')) {
      if (line.startsWith('event:')) evType = line.slice(6).trim()
      else if (line.startsWith('data:')) data += line.slice(5).trim()
    }
    if (!evType || !data) return
    try {
      const parsed = JSON.parse(data)
      if (evType === 'delta') handlers.onDelta?.(parsed as AIDeltaPayload)
      else if (evType === 'done') handlers.onDone?.(parsed as AIDonePayload)
      else if (evType === 'error') handlers.onError?.(parsed as AIErrorPayload)
    } catch (err) {
      handlers.onError?.({ message: `bad SSE payload: ${(err as Error).message}` })
    }
  }

  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      // SSE event boundary is a blank line — `\n\n`. Process every complete
      // event, leave the trailing partial in buf for the next chunk.
      let idx = buf.indexOf('\n\n')
      while (idx !== -1) {
        const eventStr = buf.slice(0, idx)
        buf = buf.slice(idx + 2)
        if (eventStr.trim() !== '') flushEvent(eventStr)
        idx = buf.indexOf('\n\n')
      }
    }
    if (buf.trim() !== '') flushEvent(buf)
  } catch (err) {
    if ((err as Error).name === 'AbortError') return
    handlers.onError?.({ message: (err as Error).message })
  }
}
