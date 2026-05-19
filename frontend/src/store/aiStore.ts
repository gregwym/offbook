import { create } from 'zustand'
import {
  createSharedThread,
  createThread,
  listMessages,
  listSharedMessages,
  listSharedThreads,
  listThreads,
  streamMessage,
  streamSharedMessage,
  type SSEHandlers,
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

// AIScope binds a store to a specific endpoint set. Personal uses
// /ai/*; household uses /h/ai/*. The store shape is identical — only the
// API calls differ — so we factor here and instantiate twice rather than
// duplicate ~150 lines.
type AIScope = {
  fetchThreads: () => Promise<AIThread[]>
  fetchMessages: (threadID: number) => Promise<AIMessage[]>
  createThread: () => Promise<AIThread>
  stream: (threadID: number, content: string, h: SSEHandlers, signal?: AbortSignal) => Promise<void>
}

function makeAIStore(scope: AIScope) {
  return create<State>((set, get) => ({
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
        const threads = await scope.fetchThreads()
        set({ threads, loadingThreads: false })
      } catch (err) {
        set({ loadingThreads: false, ...errInfo(err) })
      }
    },

    selectThread: async (id) => {
      set({ activeThreadID: id, messages: [], loadingMessages: true, error: null })
      try {
        const msgs = await scope.fetchMessages(id)
        set({ messages: msgs, loadingMessages: false })
      } catch (err) {
        set({ loadingMessages: false, ...errInfo(err) })
      }
    },

    newThread: async () => {
      set({ error: null })
      try {
        const t = await scope.createThread()
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
      if (get().streaming) return

      const now = new Date().toISOString()
      const userTurn: AIMessage = {
        id: -Date.now(),
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

      await scope.stream(
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
            set({ streaming: null })
            try {
              const [msgs, threads] = await Promise.all([
                scope.fetchMessages(activeThreadID),
                scope.fetchThreads(),
              ])
              set({ messages: msgs, threads })
            } catch (err) {
              set(errInfo(err))
            }
          },
          onError: (p) => {
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
}

// Personal-scope store: routes through /ai/* on the backend.
export const useAIStore = makeAIStore({
  fetchThreads: listThreads,
  fetchMessages: listMessages,
  createThread: () => createThread(),
  stream: streamMessage,
})

// Household-scope store: routes through /h/ai/*. Same shape — pages can
// swap which hook they call based on which scope they live in.
export const useHouseholdAIStore = makeAIStore({
  fetchThreads: listSharedThreads,
  fetchMessages: listSharedMessages,
  createThread: () => createSharedThread(),
  stream: streamSharedMessage,
})

function errInfo(err: unknown): { error: string } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return { error: r.data.error }
  }
  if (err instanceof Error) return { error: err.message }
  return { error: 'request failed' }
}
