import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { MoreHorizontal } from 'lucide-react'

export type RowAction = {
  key: string
  // Accessible name; matches the row's inline desktop button so a journey
  // finds the same action by name at either width.
  ariaLabel: string
  label: ReactNode
  onSelect: () => void
  danger?: boolean
}

// RowActionsMenu folds a row's secondary actions behind one "More actions"
// button (#554). The menu is portalled to <body> and fixed-positioned against
// the trigger, so neither the table's overflow wrapper nor a dimmed (hidden)
// row's stacking context can clip or cover it. It opens upward when the
// trigger is near the bottom of the screen. Focus moves to the first item on
// open and back to the trigger on Escape or after a choice.
export function RowActionsMenu({ label, actions }: { label: string; actions: RowAction[] }) {
  const [open, setOpen] = useState(false)
  const [style, setStyle] = useState<CSSProperties>({})
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const close = (refocus: boolean) => {
    setOpen(false)
    if (refocus) triggerRef.current?.focus()
  }

  useLayoutEffect(() => {
    if (!open || !triggerRef.current) return
    const r = triggerRef.current.getBoundingClientRect()
    const right = Math.max(8, window.innerWidth - r.right)
    const below = window.innerHeight - r.bottom
    const menuH = menuRef.current?.offsetHeight ?? 0
    setStyle(below >= menuH + 8 || r.top < menuH + 8
      ? { top: r.bottom + 4, right }
      : { bottom: window.innerHeight - r.top + 4, right })
    menuRef.current?.querySelector<HTMLButtonElement>('[role=menuitem]')?.focus({ preventScroll: true })
  }, [open])

  useEffect(() => {
    if (!open) return
    const onPointer = (e: PointerEvent) => {
      const t = e.target as Node
      if (menuRef.current?.contains(t) || triggerRef.current?.contains(t)) return
      close(false)
    }
    // The menu is pinned to where the trigger was; drop it if the page moves.
    const onMove = () => close(false)
    document.addEventListener('pointerdown', onPointer)
    window.addEventListener('scroll', onMove, true)
    window.addEventListener('resize', onMove)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
      window.removeEventListener('scroll', onMove, true)
      window.removeEventListener('resize', onMove)
    }
  }, [open])

  const onMenuKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role=menuitem]') ?? [])
    const i = items.indexOf(document.activeElement as HTMLButtonElement)
    if (e.key === 'Escape') {
      e.preventDefault()
      close(true)
    } else if (e.key === 'Tab') {
      close(false)
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const step = e.key === 'ArrowDown' ? 1 : -1
      items[(i + step + items.length) % items.length]?.focus()
    } else if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault()
      items[e.key === 'Home' ? 0 : items.length - 1]?.focus()
    }
  }

  if (actions.length === 0) return null
  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label={`More actions for ${label}`}
        aria-haspopup="menu"
        aria-expanded={open}
        className="inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-100"
      >
        <MoreHorizontal size={18} />
      </button>
      {open && createPortal(
        <div
          ref={menuRef}
          role="menu"
          aria-label={`Actions for ${label}`}
          onKeyDown={onMenuKey}
          style={style}
          className="fixed z-40 min-w-44 rounded-md border border-gray-200 bg-white py-1 text-sm shadow-lg"
        >
          {actions.map((a) => (
            <button
              key={a.key}
              type="button"
              role="menuitem"
              aria-label={a.ariaLabel}
              onClick={() => {
                close(true)
                a.onSelect()
              }}
              className={`flex min-h-11 w-full items-center gap-2 px-3 text-left hover:bg-gray-50 focus:bg-gray-100 focus:outline-none ${a.danger ? 'text-red-700' : 'text-gray-800'}`}
            >
              {a.label}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </>
  )
}
