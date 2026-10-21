import { expect } from '@playwright/test'

// clippedBoxes lists what the user can't see without a sideways scroll: the
// page itself, plus any visible element that clips or side-scrolls its own
// content (#561). A page-level scrollWidth check alone missed #470, #511,
// #512 and #554, where a table or modal wrapper hid the overflow. Intentional
// single-line ellipsis truncation and sr-only 1px boxes are not offenders.
export async function clippedBoxes(page) {
  return page.evaluate(() => {
    const out = []
    const root = document.documentElement
    if (root.scrollWidth > root.clientWidth + 1) out.push(`page: scrollWidth ${root.scrollWidth} > ${root.clientWidth}`)
    for (const el of document.body.querySelectorAll('*')) {
      if (el.clientWidth <= 1 || el.scrollWidth <= el.clientWidth + 1) continue
      const s = getComputedStyle(el)
      if (!['auto', 'scroll', 'hidden', 'clip'].includes(s.overflowX)) continue
      if (s.textOverflow === 'ellipsis') continue
      if (s.visibility === 'hidden' || el.getClientRects().length === 0) continue
      const label = el.getAttribute('aria-label') || el.getAttribute('data-testid') || (el.textContent || '').trim().slice(0, 40)
      const cls = typeof el.className === 'string' ? el.className.split(/\s+/).slice(0, 4).join('.') : ''
      out.push(`<${el.tagName.toLowerCase()}${cls ? '.' + cls : ''}> "${label}": scrollWidth ${el.scrollWidth} > ${el.clientWidth}`)
    }
    return out
  })
}

// expectNoClipping is a soft assertion so one run reports every page's
// offenders rather than stopping at the first.
export async function expectNoClipping(page, where) {
  expect.soft(await clippedBoxes(page), `${where}: content clipped or side-scrolled`).toEqual([])
}
