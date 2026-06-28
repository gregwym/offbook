// Mirror of backend service.UserSettingsView. The Claude API key is
// never sent over the wire — only a boolean flag — so the type cannot
// accidentally surface it in the UI.

// The API protocol the advisor speaks. Internal values are kept for wire
// stability (provider provenance, ai_messages.provider); the UI labels them
// by protocol (Anthropic / Ollama / OpenAI).
export type AIProvider = 'claude' | 'ollama' | 'openai'

export type UserSettingsView = {
  user_id: number
  preferred_provider: AIProvider
  // Unified per-protocol endpoint + token (#354). Endpoint is the protocol's
  // base URL (blank → provider default); the token is write-only, surfaced
  // here only as a boolean.
  api_endpoint?: string | null
  api_token_set: boolean
  preferred_model?: string | null
  // Opt-in for the daily background price refresh (#338 Phase 3). When
  // true, the held-symbol list goes to the price providers once a day
  // without a click — hence stored consent, default false.
  auto_price_refresh: boolean
}

export type UpdateUserSettingsInput = {
  preferred_provider?: AIProvider
  api_endpoint?: string
  clear_api_endpoint?: boolean
  api_token?: string
  clear_api_token?: boolean
  preferred_model?: string
  clear_preferred_model?: boolean
  auto_price_refresh?: boolean
}
