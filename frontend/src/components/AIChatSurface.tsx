import { useEffect, useMemo, useRef, useState, type ComponentType } from 'react'
import { Plus, Send, Square, Sparkles } from 'lucide-react'
import type { UseBoundStore, StoreApi } from 'zustand'
import type { AIMessage, AIModel, AIThread } from '../types/ai'

type State = {
  threads: AIThread[]
  activeThreadID: number | null
  messages: AIMessage[]
  loadingThreads: boolean
  loadingMessages: boolean
  streaming: { threadID: number; partialText: string; abort: AbortController } | null
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

export type AIChatSurfaceProps = {
  /** Hook returned by `create<State>` — personal or household scope. */
  useStore: UseBoundStore<StoreApi<State>>
  /** Header label — "AI Advisor" / "Household AI Advisor". */
  title: string
  /** One-line tagline shown in the right panel header. */
  contextTagline: string
  /** Items rendered under "NOT sent:". */
  notSent: string[]
  /** Starter prompts on empty threads. */
  suggestedPrompts: string[]
  /** Show the Claude/Ollama switcher in the header? Hide for household scope. */
  showModelSwitcher: boolean
  /** Optional icon component for the empty state. */
  EmptyIcon?: ComponentType<{ size?: number; className?: string }>
  /** Optional copy under the empty-state heading. */
  emptyDescription?: string
  /** Optional fallback for assistant rows when `message.provider` is empty. */
  assistantLabel?: string
}

// AIChatSurface is the shared three-column AI chat layout. AIPage and
// HouseholdAIPage are thin wrappers; the only thing that differs is the
// backing store + the labels in the chrome. Keeping one component means
// the personal + household chats stay in lockstep when we polish them.
export function AIChatSurface({
  useStore,
  title,
  contextTagline,
  notSent,
  suggestedPrompts,
  showModelSwitcher,
  EmptyIcon = Sparkles,
  emptyDescription,
  assistantLabel,
}: AIChatSurfaceProps) {
  const {
    threads,
    activeThreadID,
    messages,
    loadingThreads,
    loadingMessages,
    streaming,
    error,
    model,
    fetchThreads,
    selectThread,
    newThread,
    sendMessage,
    cancelStreaming,
    setModel,
    clearError,
  } = useStore()
  const [draft, setDraft] = useState('')
  const messagesEnd = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    void fetchThreads()
  }, [fetchThreads])

  useEffect(() => {
    messagesEnd.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, streaming?.partialText])

  const activeThread = useMemo(
    () => threads.find((t) => t.id === activeThreadID) ?? null,
    [threads, activeThreadID],
  )

  const lastContextSnapshot = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i]
      if (m.role === 'assistant' && m.context_snapshot != null) {
        return m.context_snapshot
      }
    }
    return null
  }, [messages])

  const handleSend = async () => {
    if (!draft.trim() || streaming) return
    if (activeThreadID == null) {
      await newThread()
    }
    const content = draft
    setDraft('')
    await sendMessage(content)
  }

  const handleSuggested = async (prompt: string) => {
    if (streaming) return
    if (activeThreadID == null) {
      await newThread()
    }
    await sendMessage(prompt)
  }

  return (
    <div className="-mx-8 -my-8 grid grid-cols-[220px_1fr_280px] gap-0 h-[calc(100vh-0px)]">
      <aside className="border-r border-gray-200 bg-white flex flex-col">
        <div className="px-3 py-3 border-b border-gray-200 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-gray-900">Threads</h2>
          <button
            type="button"
            onClick={() => void newThread()}
            className="inline-flex items-center gap-1 rounded-md bg-indigo-600 px-2 py-1 text-xs font-medium text-white hover:bg-indigo-700"
            title="New thread"
          >
            <Plus size={12} /> New
          </button>
        </div>
        <div className="flex-1 overflow-auto">
          {loadingThreads && threads.length === 0 && (
            <div className="px-3 py-4 text-xs text-gray-400">Loading…</div>
          )}
          {!loadingThreads && threads.length === 0 && (
            <div className="px-3 py-4 text-xs text-gray-400">
              No threads yet. Start one to ask the AI a question.
            </div>
          )}
          {threads.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => void selectThread(t.id)}
              className={[
                'block w-full text-left px-3 py-2 text-sm border-b border-gray-100',
                t.id === activeThreadID
                  ? 'bg-indigo-50 text-indigo-900'
                  : 'hover:bg-gray-50 text-gray-700',
              ].join(' ')}
            >
              <div className="truncate">{t.title || `Thread ${t.id}`}</div>
              <div className="text-[11px] text-gray-400">
                {new Date(t.updated_at).toLocaleString()}
              </div>
            </button>
          ))}
        </div>
      </aside>

      <section className="flex flex-col bg-gray-50">
        <header className="flex items-center justify-between border-b border-gray-200 bg-white px-4 py-3">
          <div>
            <h1 className="text-base font-semibold text-gray-900">{title}</h1>
            <p className="text-xs text-gray-500">
              {activeThread
                ? activeThread.title || `Thread ${activeThread.id}`
                : 'Start a new thread or pick one from the left.'}
            </p>
          </div>
          {showModelSwitcher && <ModelSwitcher value={model} onChange={setModel} />}
        </header>

        {error && (
          <div className="mx-4 mt-3 flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
            <span>{error}</span>
            <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">
              ×
            </button>
          </div>
        )}

        <div className="flex-1 overflow-auto px-4 py-4">
          {loadingMessages && messages.length === 0 && (
            <div className="text-center text-xs text-gray-400">Loading messages…</div>
          )}
          {!loadingMessages && messages.length === 0 && (
            <EmptyState
              onSuggested={handleSuggested}
              disabled={!!streaming}
              prompts={suggestedPrompts}
              Icon={EmptyIcon}
              description={emptyDescription}
            />
          )}
          {messages.map((m) => (
            <MessageBubble key={m.id} message={m} assistantLabel={assistantLabel} />
          ))}
          {streaming && streaming.partialText !== '' && (
            <MessageBubble
              message={{
                id: -1,
                thread_id: streaming.threadID,
                role: 'assistant',
                content: streaming.partialText,
                created_at: new Date().toISOString(),
              }}
              streaming
              assistantLabel={assistantLabel}
            />
          )}
          <div ref={messagesEnd} />
        </div>

        <footer className="border-t border-gray-200 bg-white p-3">
          <div className="flex items-end gap-2">
            <textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  void handleSend()
                }
              }}
              rows={2}
              placeholder="Ask about your finances — e.g. can I afford a $2k flight in October?"
              className="flex-1 resize-none rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
            />
            {streaming ? (
              <button
                type="button"
                onClick={cancelStreaming}
                className="inline-flex items-center gap-1 rounded-md bg-gray-200 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-300"
              >
                <Square size={14} /> Stop
              </button>
            ) : (
              <button
                type="button"
                onClick={() => void handleSend()}
                disabled={!draft.trim()}
                className="inline-flex items-center gap-1 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
              >
                <Send size={14} /> Send
              </button>
            )}
          </div>
        </footer>
      </section>

      <aside className="border-l border-gray-200 bg-white flex flex-col">
        <div className="px-3 py-3 border-b border-gray-200">
          <h2 className="text-sm font-semibold text-gray-900">Context sent</h2>
          <p className="text-[11px] text-gray-500 leading-tight mt-1">{contextTagline}</p>
        </div>
        <div className="flex-1 overflow-auto px-3 py-3 text-[11px]">
          {lastContextSnapshot ? (
            <pre className="whitespace-pre-wrap text-gray-700">
              {JSON.stringify(lastContextSnapshot, null, 2)}
            </pre>
          ) : (
            <p className="text-gray-400">
              No context yet — send a message to see the snapshot the AI received.
            </p>
          )}
        </div>
        <div className="border-t border-gray-200 px-3 py-3 text-[11px]">
          <div className="font-semibold text-red-700 mb-1">NOT sent:</div>
          <ul className="space-y-0.5 text-gray-600">
            {notSent.map((item) => (
              <li key={item} className="line-through">
                {item}
              </li>
            ))}
          </ul>
        </div>
      </aside>
    </div>
  )
}

function ModelSwitcher({ value, onChange }: { value: AIModel; onChange: (m: AIModel) => void }) {
  return (
    <div className="inline-flex rounded-md border border-gray-200 bg-gray-50 p-0.5 text-xs">
      {(['claude', 'ollama'] as const).map((m) => (
        <button
          key={m}
          type="button"
          onClick={() => onChange(m)}
          className={[
            'px-2 py-1 rounded',
            value === m ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-700',
          ].join(' ')}
        >
          {m === 'claude' ? 'Claude' : 'Ollama'}
        </button>
      ))}
    </div>
  )
}

function EmptyState({
  onSuggested,
  disabled,
  prompts,
  Icon,
  description,
}: {
  onSuggested: (p: string) => void
  disabled: boolean
  prompts: string[]
  Icon: ComponentType<{ size?: number; className?: string }>
  description?: string
}) {
  return (
    <div className="mx-auto max-w-md text-center py-8">
      <Icon size={28} className="mx-auto text-indigo-500 mb-2" />
      <h3 className="text-base font-medium text-gray-900">Ask about your finances</h3>
      <p className="text-xs text-gray-500 mt-1">
        {description ??
          'The assistant only sees anonymized aggregates from your data — no holder names, no account numbers.'}
      </p>
      <div className="mt-4 grid gap-2">
        {prompts.map((p) => (
          <button
            key={p}
            type="button"
            disabled={disabled}
            onClick={() => onSuggested(p)}
            className="rounded-md border border-gray-200 bg-white px-3 py-2 text-left text-sm text-gray-700 hover:border-indigo-300 hover:bg-indigo-50 disabled:opacity-40"
          >
            {p}
          </button>
        ))}
      </div>
    </div>
  )
}

function MessageBubble({
  message,
  streaming,
  assistantLabel,
}: {
  message: AIMessage
  streaming?: boolean
  assistantLabel?: string
}) {
  const isUser = message.role === 'user'
  return (
    <div className={['mb-3 flex', isUser ? 'justify-end' : 'justify-start'].join(' ')}>
      <div
        className={[
          'max-w-[80%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap',
          isUser ? 'bg-indigo-600 text-white' : 'bg-white border border-gray-200 text-gray-900',
        ].join(' ')}
      >
        <div
          className={[
            'text-[10px] uppercase tracking-wide mb-1',
            isUser ? 'text-indigo-200' : 'text-gray-400',
          ].join(' ')}
        >
          {isUser
            ? `You${message.user_id ? ` · #${message.user_id}` : ''}`
            : message.provider || assistantLabel || 'Assistant'}
          {streaming && <span className="ml-1 animate-pulse">…</span>}
        </div>
        {message.content}
      </div>
    </div>
  )
}
