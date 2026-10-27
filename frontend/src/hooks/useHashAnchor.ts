import { useEffect } from 'react'

// How long a hash landing keeps its target pinned while the page settles.
export const HASH_ANCHOR_SETTLE_MS = 4000

const USER_SCROLL_EVENTS = ['wheel', 'touchstart', 'keydown', 'pointerdown'] as const

// useHashAnchor scrolls the element named by `hash` into view and keeps it
// there while the page settles: sections above it that finish loading later
// grow and would otherwise push it down, off-screen on mobile (#647). It
// re-scrolls whenever the target's parent (the page content) resizes, until
// the user scrolls or taps, or HASH_ANCHOR_SETTLE_MS passes. The parent, not
// <body>, because AppShell scrolls inside <main> and body never grows. Falls
// back to the single scroll where ResizeObserver is missing.
export function useHashAnchor(hash: string) {
  useEffect(() => {
    if (!hash) return
    const id = hash.slice(1)
    const pin = () => document.getElementById(id)?.scrollIntoView?.({ block: 'start' })
    pin()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(pin)
    observer.observe(document.getElementById(id)?.parentElement ?? document.body)
    const stop = () => {
      observer.disconnect()
      window.clearTimeout(timer)
      USER_SCROLL_EVENTS.forEach((e) => window.removeEventListener(e, stop))
    }
    const timer = window.setTimeout(stop, HASH_ANCHOR_SETTLE_MS)
    USER_SCROLL_EVENTS.forEach((e) => window.addEventListener(e, stop, { passive: true }))
    return stop
  }, [hash])
}
