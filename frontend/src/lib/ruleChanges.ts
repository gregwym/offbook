import type { ApplyResult } from '../types/categorizationRule'

// Words for what rules change (#552), shared by the Rules page's
// preview/apply banner and the rule form's live impact so both describe a
// rule's effect the same way: by action, not as "re-categorized" for all.

export type RuleChangeCounts = {
  category: number
  renamed: number
  hidden: number
  reviewed: number
  tagged: number
}

type Tense = 'would' | 'did'

// The first part carries the verb ("3 would change category, 2 renamed");
// zero counts are omitted. Returns '' when nothing visible changes.
export function describeRuleChanges(c: RuleChangeCounts, tense: Tense): string {
  const parts: [number, string, string, string][] = [
    // [count, would-first, did-first, later]
    [c.category, 'would change category', 'changed category', 'changed category'],
    [c.renamed, 'would be renamed', 'renamed', 'renamed'],
    [c.hidden, 'would be hidden', 'hidden', 'hidden'],
    [c.reviewed, 'would be marked reviewed', 'marked reviewed', 'marked reviewed'],
    [c.tagged, 'would be tagged', 'tagged', 'tagged'],
  ]
  return parts
    .filter(([n]) => n > 0)
    .map(([n, wouldFirst, didFirst, later], i) => `${n} ${i > 0 ? later : tense === 'would' ? wouldFirst : didFirst}`)
    .join(', ')
}

// "1 manual pick stays" / "2 manual picks stayed"; '' for zero.
export function describeManualPicks(n: number, tense: Tense): string {
  if (n <= 0) return ''
  const picks = n === 1 ? 'manual pick' : 'manual picks'
  if (tense === 'did') return `${n} ${picks} stayed as ${n === 1 ? 'it was' : 'they were'}`
  return `${n} ${picks} ${n === 1 ? 'stays' : 'stay'} as ${n === 1 ? 'it is' : 'they are'}`
}

// Says what a bulk preview/apply changes per action (#552), e.g.
// "Preview (3 of 40 transactions): 3 would change category, 2 renamed; 1
// manual pick stays as it is. Nothing has been changed."
export function formatApplyResult(r: ApplyResult): string {
  const tense = r.dry_run ? 'would' : 'did'
  const changes = describeRuleChanges(
    {
      category: r.recategorized ?? 0,
      renamed: r.renamed ?? 0,
      hidden: r.hidden ?? 0,
      reviewed: r.marked_reviewed ?? 0,
      tagged: r.tagged ?? 0,
    },
    tense,
  )
  const manual = describeManualPicks(r.skipped_manual, tense)
  let body: string
  if (r.updated === 0) {
    body = r.dry_run
      ? `Preview: none of ${r.scanned} transactions would change`
      : `Nothing to update in ${r.scanned} transactions`
    if (manual) body += `; ${manual}`
  } else {
    const what =
      changes ||
      (r.dry_run ? 'they would only record a rule as their source' : 'they now record a rule as their source')
    body = r.dry_run
      ? `Preview (${r.updated} of ${r.scanned} transactions): ${what}`
      : `Updated ${r.updated} of ${r.scanned} transactions: ${what}`
    if (manual) body += `; ${manual}`
  }
  return r.dry_run ? `${body}. Nothing has been changed.` : `${body}.`
}
