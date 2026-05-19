import { create } from 'zustand'
import {
  createThread as apiCreate,
  listMessages,
  listThreads,
  streamMessage,
} from '../api/ai'
import type { AIMessage, AIModel, AIThread } from '../types/ai'

const MODEL_KEY = 'offbook.ai.model'

function loadModel(): AIModel {
  if (typeof window === 'undefined') return 'claude'
  const v = window.localStorage.getItem(MODEL_KEY)
  return v === 'ollama' ? 'ollama' : 'claude'
}

type StreamingState = {
  threadID: number
  partialText: string
  abort: AbortController
}

type State = {
  threads: AIThread[]
  activeThreadID: number | null
  messages: AIMessage[]
  loadingThreads: boolean
  loadingMessages: boolean
  streaming: StreamingState | null
  error: string | null
  model: AIModel

  fetchThreads: () => Promise<void>
  selectThread: (id: number) => Promise<void>
  newThread: () => Promise<AIThread>
  sendMessage: (content: string) => Promise<void>
  cancelStreaming: () => void
  setModel: (m: AIModel) => void
  clearError: () => void
}

export const useAIStore = create<State>((set, get) => ({
  threads: [],
  activeThreadID: null,
  messages: [],
  loadingThreads: false,
  loadingMessages: false,
  streaming: null,
  error: null,
  model: loadModel(),

  fetchThreads: async () => {
    set({ loadingThreads: true, error: null })
    try {
      const threads = await listThreads()
      set({ threads, loadingThreads: false })
    } catch (err) {
      set({ loadingThreads: false, ...errInfo(err) })
    }
  },

  selectThread: async (id) => {
    set({ activeThreadID: id, messages: [], loadingMessages: true, error: null })
    try {
      const msgs = await listMessages(id)
      set({ messages: msgs, loadingMessages: false })
    } catch (err) {
      set({ loadingMessages: false, ...errInfo(err) })
    }
  },

  newThread: async () => {
    set({ error: null })
    try {
      const t = await apiCreate()
      set({
        threads: [t, ...get().threads],
        activeThreadID: t.id,
        messages: [],
      })
      return t
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  sendMessage: async (content) => {
    const { activeThreadID } = get()
    if (!activeThreadID || !content.trim()) return
    if (get().streaming) return // already streaming — caller shouldn't double-send

    // Optimistically append the user message and an empty assistant
    // placeholder. The placeholder accumulates streamed text in-place so
    // the UI doesn't flash a "no messages" gap mid-stream.
    const now = new Date().toISOString()
    const userTurn: AIMessage = {
      id: -Date.now(), // negative ID flags "optimistic"
      thread_id: activeThreadID,
      role: 'user',
      content,
      created_at: now,
    }
    set({
      messages: [...get().messages, userTurn],
      streaming: {
        threadID: activeThreadID,
        partialText: '',
        abort: new AbortController(),
      },
      error: null,
    })

    await streamMessage(
      activeThreadID,
      content,
      {
        onDelta: (p) => {
          const s = get().streaming
          if (!s) return
          const partial = s.partialText + p.text
          set({ streaming: { ...s, partialText: partial } })
        },
        onDone: async () => {
          // Reload messages so the server-canonical IDs + timestamps replace
          // the optimistic rows. Also refresh thread list so updated_at order
          // is correct.
          set({ streaming: null })
          try {
            const [msgs, threads] = await Promise.all([
              listMessages(activeThreadID),
              listThreads(),
            ])
            set({ messages: msgs, threads })
          } catch (err) {
            set(errInfo(err))
          }
        },
        onError: (p) => {
          // Roll back the optimistic placeholder messages so the user can
          // retry without two phantom turns hanging around.
          set({
            streaming: null,
            messages: get().messages.filter((m) => m.id !== userTurn.id),
            error: p.message,
          })
        },
      },
      get().streaming?.abort.signal,
    )
  },

  cancelStreaming: () => {
    const s = get().streaming
    if (!s) return
    s.abort.abort()
    set({ streaming: null })
  },

  setModel: (m) => {
    if (typeof window !== 'undefined') window.localStorage.setItem(MODEL_KEY, m)
    set({ model: m })
  },

  clearError: () => set({ error: null }),
}))

function errInfo(err: unknown): { error: string } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return { error: r.data.error }
  }
  if (err instanceof Error) return { error: err.message }
  return { error: 'request failed' }
}
