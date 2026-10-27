import { renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { HASH_ANCHOR_SETTLE_MS, useHashAnchor } from './useHashAnchor'

// A section above the target that finishes loading later grows the page;
// the target must be scrolled back into view (#647).
describe('useHashAnchor', () => {
  let resize: (() => void) | null
  let target: HTMLElement
  let scroll: ReturnType<typeof vi.fn<(arg?: boolean | ScrollIntoViewOptions) => void>>

  beforeEach(() => {
    vi.useFakeTimers()
    resize = null
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(cb: () => void) {
          resize = cb
        }
        observe() {}
        disconnect() {
          resize = null
        }
      },
    )
    target = document.createElement('section')
    target.id = 'linked-institutions'
    scroll = vi.fn<(arg?: boolean | ScrollIntoViewOptions) => void>()
    target.scrollIntoView = scroll
    document.body.appendChild(target)
  })

  afterEach(() => {
    target.remove()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('scrolls on landing and again when the page grows above it', () => {
    renderHook(() => useHashAnchor('#linked-institutions'))
    expect(scroll).toHaveBeenCalledTimes(1)
    resize?.()
    expect(scroll).toHaveBeenCalledTimes(2)
    expect(scroll).toHaveBeenLastCalledWith({ block: 'start' })
  })

  it('lets go once the user scrolls', () => {
    renderHook(() => useHashAnchor('#linked-institutions'))
    window.dispatchEvent(new Event('wheel'))
    expect(resize).toBeNull()
    expect(scroll).toHaveBeenCalledTimes(1)
  })

  it('lets go once the page has had time to settle', () => {
    renderHook(() => useHashAnchor('#linked-institutions'))
    vi.advanceTimersByTime(HASH_ANCHOR_SETTLE_MS)
    expect(resize).toBeNull()
  })

  it('does nothing without a hash', () => {
    renderHook(() => useHashAnchor(''))
    expect(scroll).not.toHaveBeenCalled()
    expect(resize).toBeNull()
  })
})
