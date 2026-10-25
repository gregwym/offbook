import { ChevronLeft, ChevronRight } from 'lucide-react'
import { currentMonth, monthLabel, shiftMonth } from '../lib/month'

// MonthStepper steps back through earlier months (#577, #607).
// month null is the current month; "next" stops there.
export function MonthStepper({
  month,
  onChange,
  label,
  className = 'mt-4',
}: {
  month: string | null
  onChange: (month: string | null) => void
  // Accessible name for the control group, e.g. "Budget month".
  label: string
  className?: string
}) {
  const shown = month ?? currentMonth()
  const step = (delta: number) => {
    const next = shiftMonth(shown, delta)
    onChange(next >= currentMonth() ? null : next)
  }
  const btn =
    'inline-flex h-10 w-10 items-center justify-center rounded-md border border-gray-300 bg-white text-gray-600 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-40'
  return (
    <nav aria-label={label} className={`${className} flex flex-wrap items-center gap-2`}>
      <button type="button" className={btn} onClick={() => step(-1)} aria-label="Previous month">
        <ChevronLeft size={16} />
      </button>
      <span className="min-w-[9rem] text-center text-sm font-medium text-gray-900" aria-live="polite">
        {monthLabel(shown)}
      </span>
      <button type="button" className={btn} onClick={() => step(1)} disabled={month == null} aria-label="Next month">
        <ChevronRight size={16} />
      </button>
      {month && (
        <button type="button" onClick={() => onChange(null)} className="text-sm font-medium text-indigo-600 hover:text-indigo-800">
          This month
        </button>
      )}
    </nav>
  )
}
