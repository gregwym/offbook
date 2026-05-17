import { useEffect, useState } from 'react'

// useDebounce returns a value that lags behind `value` by `delayMs`.
// Used for search inputs so we don't issue a request per keystroke.
export function useDebounce<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(t)
  }, [value, delayMs])
  return debounced
}
