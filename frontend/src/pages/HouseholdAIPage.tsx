import { Home } from 'lucide-react'
import { AIChatSurface } from '../components/AIChatSurface'
import { useHouseholdAIStore } from '../store/aiStore'

// Starter prompts target the household aggregator's surfaces — shared
// dashboard, shared budgets, shared goals — so the model has obvious
// data to ground on for the first turn.
const HOUSEHOLD_PROMPTS = [
  'How is the household tracking against its shared budgets this month?',
  'What did we spend on across the household last month?',
  'Are our shared savings goals on schedule?',
  'Which member is contributing most to household net worth?',
]

export function HouseholdAIPage() {
  return (
    <AIChatSurface
      useStore={useHouseholdAIStore}
      title="Household AI Advisor"
      contextTagline="Household-aggregated snapshot the model received. Private accounts and in-grace members are excluded."
      notSent={[
        'private (un-shared) accounts',
        'holder names',
        'institutions',
        "per-member personal threads (each member's chat is private)",
        'per-transaction rows',
      ]}
      suggestedPrompts={HOUSEHOLD_PROMPTS}
      showModelSwitcher={false}
      EmptyIcon={Home}
      emptyDescription="Threads here are shared with every active member of your household. Use the personal AI Advisor for private questions."
      assistantLabel="Household assistant"
    />
  )
}
