import { useEffect, useState } from 'react'

// useMinWidth reports whether the viewport is at least `px` wide, tracking
// resizes. Use it only where a component must mount once (e.g. it fetches
// or holds state); pure layout differences belong in Tailwind breakpoints.
// Defaults to true where matchMedia is unavailable (jsdom).
export function useMinWidth(px: number): boolean {
  const query = `(min-width: ${px}px)`
  const [matches, setMatches] = useState(() =>
    typeof window === 'undefined' || typeof window.matchMedia !== 'function' ? true : window.matchMedia(query).matches,
  )
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mql = window.matchMedia(query)
    const onChange = () => setMatches(mql.matches)
    onChange()
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])
  return matches
}
