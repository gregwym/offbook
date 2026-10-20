import type { ReactNode } from 'react'

const widths = {
  md: 'max-w-md',
  lg: 'max-w-lg',
  '2xl': 'max-w-2xl',
} as const

// ModalShell is the backdrop + panel every form modal sits in. It stacks at
// z-50, above the mobile top bar (z-30) and drawer backdrop (z-40), so a tall
// modal's title is never hidden behind the bar. The panel is capped to the
// viewport and scrolls, so its action buttons stay reachable at 390px however
// long the form gets (#548). A modal that wants only its body to scroll can
// make the body `min-h-0 flex-1 overflow-y-auto`; the panel is a flex column.
//
// `sheet` docks the panel to the bottom edge on phones (from sm: it centres
// like the rest).
export function ModalShell({
  children,
  size = 'lg',
  sheet = false,
  'aria-label': ariaLabel,
  'aria-labelledby': ariaLabelledBy,
}: {
  children: ReactNode
  size?: keyof typeof widths
  sheet?: boolean
  'aria-label'?: string
  'aria-labelledby'?: string
}) {
  const backdrop = sheet
    ? 'fixed inset-0 z-50 flex items-end justify-center bg-black/40 p-0 sm:items-center sm:p-4'
    : 'fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4'
  const panel = sheet
    ? 'max-h-[90dvh] rounded-t-lg sm:max-h-[calc(100dvh-2rem)] sm:rounded-lg'
    : 'max-h-[calc(100dvh-2rem)] rounded-lg'
  return (
    <div className={backdrop}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledBy}
        className={`flex w-full ${widths[size]} ${panel} flex-col overflow-y-auto bg-white shadow-xl`}
      >
        {children}
      </div>
    </div>
  )
}
