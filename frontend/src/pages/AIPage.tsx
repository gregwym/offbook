import { Sparkles } from 'lucide-react'
import { AIChatSurface } from '../components/AIChatSurface'
import { useAIStore } from '../store/aiStore'

// Starter prompts shown on empty personal threads. Picked to exercise
// the aggregator surfaces in context_builder.go (net worth, spend,
// budgets, goals, allocation) so users see the assistant pulling real
// data right away.
const SUGGESTED_PROMPTS = [
  'How am I doing against my budgets this month?',
  'What was my biggest spending category last month?',
  'Am I on track for my savings goals?',
  'Is my asset allocation reasonable for my age?',
]

export function AIPage() {
  return (
    <AIChatSurface
      useStore={useAIStore}
      title="AI Advisor"
      contextTagline="The exact aggregate snapshot the model received for the most recent turn."
      notSent={['account numbers', 'holder names', 'institutions', 'per-transaction rows']}
      suggestedPrompts={SUGGESTED_PROMPTS}
      showModelSwitcher
      EmptyIcon={Sparkles}
    />
  )
}
