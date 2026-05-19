// Mirror of backend service.UserSettingsView. The Claude API key is
// never sent over the wire — only a boolean flag — so the type cannot
// accidentally surface it in the UI.

export type UserSettingsView = {
  user_id: number
  claude_api_key_set: boolean
  ollama_base_url?: string | null
  preferred_provider: 'claude' | 'ollama'
  preferred_model?: string | null
}

export type UpdateUserSettingsInput = {
  claude_api_key?: string
  clear_claude_api_key?: boolean
  ollama_base_url?: string
  clear_ollama_url?: boolean
  preferred_provider?: 'claude' | 'ollama'
  preferred_model?: string
  clear_preferred_model?: boolean
}
