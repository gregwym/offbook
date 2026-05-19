// Mirror of backend model.AIThread / model.AIMessage. Keep in sync with
// backend/internal/model/ai_thread.go and ai_message.go.

export type AIThread = {
  id: number
  user_id: number
  household_id?: number | null
  shared_with_household: boolean
  title?: string | null
  created_at: string
  updated_at: string
}

export type AIRole = 'user' | 'assistant' | 'system'

export type AIMessage = {
  id: number
  thread_id: number
  // user_id is set on user-role messages in shared threads so the UI can
  // attribute "who said what" across members. Null on assistant messages
  // and on pre-migration-000011 user messages.
  user_id?: number | null
  role: AIRole
  content: string
  context_snapshot?: unknown // JSONB on the server; opaque to the UI except for the preview panel
  provider?: string | null
  model_name?: string | null
  created_at: string
}

// SSE event payloads mirror backend service/ai/service.go.
export type AIDeltaPayload = { text: string }

export type AIDonePayload = {
  finish_reason?: string
  input_tokens?: number
  output_tokens?: number
  message_id: number
}

export type AIErrorPayload = { message: string; code?: string }

// AIModel is the user-facing choice in the model switcher. Settings page
// (#131) will own real persistence; this page uses localStorage in the
// meantime.
export type AIModel = 'claude' | 'ollama'
