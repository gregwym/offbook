import type { Category } from './category'

// Mirror of backend model.AICategorizationVerdict — one cached AI
// merchant→category decision (#366). Read-only; the source list for the
// Rules page "promote to rule" affordance.
export type AICategorizationVerdict = {
  id: number
  user_id: number
  merchant_key: string
  category_id: number
  confidence: number
  provider: string
  created_at: string
  updated_at: string
  category?: Category | null
}
