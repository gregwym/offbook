import { useEffect, useState, type ReactNode } from 'react'
import { previewDraftRule } from '../api/categorizationRules'
import { listTransactionTags } from '../api/transactions'
import { MAX_TAG_LENGTH, mergeTags } from '../lib/tags'
import type { TagCount } from '../types/transaction'
import type { Category } from '../types/category'
import {
  MATCH_TYPES,
  type CategorizationRule,
  type CreateRuleInput,
  type DraftPreview,
  type MatchType,
  type UpdateRuleInput,
} from '../types/categorizationRule'

// Defaults used in 'create' mode. Edit mode ignores defaults and reads
// initial values from the rule itself. Every field is optional so callers
// can pre-fill as much or as little as makes sense (e.g. the transactions-
// table "create rule from this txn" shortcut fills pattern + match_type +
// category_id but leaves the rest at sensible defaults).
export type RuleFormDefaults = {
  pattern?: string
  match_type?: MatchType
  category_id?: number
  priority?: number
  is_active?: boolean
  rename_merchant?: string
  hide_matches?: boolean
  mark_reviewed?: boolean
  add_tags?: string[]
}

// Mirrors the backend's maxRenameMerchant.
const MAX_RENAME = 200

type Props = {
  mode: 'create' | 'edit'
  categories: Category[]
  rule?: CategorizationRule
  defaults?: RuleFormDefaults
  onClose: () => void
  onSubmit: (input: CreateRuleInput | UpdateRuleInput) => Promise<void>
}

export function RuleFormModal({ mode, categories, rule, defaults, onClose, onSubmit }: Props) {
  const [pattern, setPattern] = useState(rule?.pattern ?? defaults?.pattern ?? '')
  const [matchType, setMatchType] = useState<MatchType>(
    rule?.match_type ?? defaults?.match_type ?? 'contains',
  )
  const [categoryID, setCategoryID] = useState<number | ''>(
    rule?.category_id ?? defaults?.category_id ?? '',
  )
  const [priority, setPriority] = useState<number>(
    rule?.priority ?? defaults?.priority ?? 10,
  )
  const [isActive, setIsActive] = useState(rule?.is_active ?? defaults?.is_active ?? true)
  const [renameMerchant, setRenameMerchant] = useState(
    rule?.rename_merchant ?? defaults?.rename_merchant ?? '',
  )
  const rename = renameMerchant.trim()
  const [hideMatches, setHideMatches] = useState(rule?.hide_matches ?? defaults?.hide_matches ?? false)
  const [markReviewed, setMarkReviewed] = useState(rule?.mark_reviewed ?? defaults?.mark_reviewed ?? false)
  // Tags the rule adds (#546), typed comma-separated and normalized like the
  // transaction tag editor; the user's existing tags are one-tap suggestions.
  const [tagsText, setTagsText] = useState((rule?.add_tags ?? defaults?.add_tags ?? []).join(', '))
  const [knownTags, setKnownTags] = useState<TagCount[]>([])
  useEffect(() => {
    let live = true
    listTransactionTags()
      .then((t) => { if (live) setKnownTags(t) })
      .catch(() => {})
    return () => { live = false }
  }, [])
  const parsedTags = mergeTags([], tagsText, knownTags)
  const ruleTags = parsedTags.next ?? []
  // Stable string form of ruleTags for the preview effect's dependencies.
  const tagsKey = ruleTags.join('\n')
  const tagSuggestions = knownTags.filter((k) => !ruleTags.some((t) => t.toLowerCase() === k.name.toLowerCase())).slice(0, 8)
  const addSuggestion = (name: string) => setTagsText((cur) => (cur.trim() ? `${cur.trim().replace(/,$/, '')}, ${name}` : name))
  const [submitting, setSubmitting] = useState(false)
  // We split form errors by field when the backend gives us a code; otherwise
  // surface as a generic banner above the form.
  const [error, setError] = useState<string | null>(null)
  const [patternError, setPatternError] = useState<string | null>(null)
  const [categoryError, setCategoryError] = useState<string | null>(null)
  const [renameError, setRenameError] = useState<string | null>(null)
  const tagsError = parsedTags.error

  // Live impact of the draft rule: debounced, cancels stale requests, and
  // never blocks saving. Results are keyed by the draft they describe, so a
  // result for an older draft reads as "loading" rather than stale.
  const cat = Number(categoryID)
  const draftReady = pattern.trim() !== '' && Number.isInteger(cat) && cat > 0 && priority >= 0
  const draftKey = JSON.stringify([pattern.trim(), matchType, cat, priority, isActive, rename, hideMatches, markReviewed, tagsKey, rule?.id ?? null])
  const [impact, setImpact] = useState<{ key: string; data: DraftPreview | null } | null>(null)
  useEffect(() => {
    if (!draftReady) return
    const ctrl = new AbortController()
    const timer = setTimeout(() => {
      previewDraftRule(
        {
          pattern: pattern.trim(),
          match_type: matchType,
          category_id: cat,
          priority,
          is_active: isActive,
          ...(rename ? { rename_merchant: rename } : {}),
          ...(hideMatches ? { hide_matches: true } : {}),
          ...(markReviewed ? { mark_reviewed: true } : {}),
          ...(tagsKey ? { add_tags: tagsKey.split('\n') } : {}),
        },
        rule?.id,
        ctrl.signal,
      )
        .then((data) => setImpact({ key: draftKey, data }))
        .catch(() => {
          if (!ctrl.signal.aborted) setImpact({ key: draftKey, data: null })
        })
    }, 400)
    return () => {
      clearTimeout(timer)
      ctrl.abort()
    }
  }, [draftKey, draftReady, pattern, matchType, cat, priority, isActive, rename, hideMatches, markReviewed, tagsKey, rule?.id])
  const current = draftReady && impact?.key === draftKey ? impact : null
  const impactData = current?.data ?? null
  const impactLoading = draftReady && isActive && current === null

  const submit = async () => {
    setError(null)
    setPatternError(null)
    setCategoryError(null)
    setRenameError(null)
    if (!pattern.trim()) {
      setPatternError('Pattern is required.')
      return
    }
    if (categoryID === '' || !Number.isInteger(categoryID) || categoryID <= 0) {
      setCategoryError('Pick a category.')
      return
    }
    if (priority < 0) {
      setError('Priority must be >= 0.')
      return
    }
    if (rename.length > MAX_RENAME) {
      setRenameError(`Keep the merchant name to ${MAX_RENAME} characters or fewer.`)
      return
    }
    if (tagsError) return
    setSubmitting(true)
    try {
      await onSubmit({
        pattern: pattern.trim(),
        match_type: matchType,
        category_id: Number(categoryID),
        priority,
        is_active: isActive,
        // Editing sends '' to clear a rename; creating omits a blank one.
        ...(rename || rule?.rename_merchant ? { rename_merchant: rename } : {}),
        hide_matches: hideMatches,
        mark_reviewed: markReviewed,
        add_tags: ruleTags,
      })
    } catch (err) {
      const { code, message } = extractErr(err)
      if (code === 'INVALID_REGEX') {
        setPatternError(message)
      } else if (code === 'UNKNOWN_CATEGORY') {
        setCategoryError(message)
      } else {
        setError(message)
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      {/* Capped to the viewport with only the body scrolling, so Create/Save
          stay reachable on a phone however long the form gets. */}
      <div className="flex max-h-[calc(100dvh-2rem)] w-full max-w-lg flex-col rounded-lg bg-white shadow-xl">
        <div className="shrink-0 border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          {mode === 'create' ? 'New rule' : `Edit "${rule?.pattern}"`}
        </div>
        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-4">
          {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
          <Field label="Pattern" error={patternError}>
            <input
              className={inputClass}
              value={pattern}
              onChange={(e) => setPattern(e.target.value)}
              placeholder={matchType === 'regex' ? 'e.g. ^AMZN\\s' : 'e.g. WHOLEFDS'}
            />
            <p className="mt-1 text-xs text-gray-500">
              {matchType === 'contains' && 'Case-insensitive substring match on description or merchant.'}
              {matchType === 'exact' && 'Case-insensitive exact match on description or merchant.'}
              {matchType === 'regex' && 'Go regexp; case-sensitive unless you prefix (?i).'}
            </p>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Match type">
              <select className={inputClass} value={matchType} onChange={(e) => setMatchType(e.target.value as MatchType)}>
                {MATCH_TYPES.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </Field>
            <Field label="Priority (higher runs first)">
              <input
                className={inputClass}
                type="number"
                min={0}
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
              />
            </Field>
          </div>
          <Field label="Category" error={categoryError}>
            <select
              className={inputClass}
              value={categoryID === '' ? '' : String(categoryID)}
              onChange={(e) => setCategoryID(e.target.value === '' ? '' : Number(e.target.value))}
            >
              <option value="">— pick one —</option>
              {categories.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          </Field>
          <Field label="Rename merchant to (optional)" error={renameError}>
            <input
              className={inputClass}
              value={renameMerchant}
              onChange={(e) => setRenameMerchant(e.target.value)}
              placeholder="e.g. Amazon"
            />
            <p className="mt-1 text-xs text-gray-500">
              Matching transactions show this merchant in the list, Top merchants and Recurring. Leave blank to keep the bank&rsquo;s name.
            </p>
          </Field>
          <div>
            <label className="flex items-center gap-2 text-sm text-gray-700">
              <input type="checkbox" checked={hideMatches} onChange={(e) => setHideMatches(e.target.checked)} />
              Also hide matching transactions
            </label>
            <p className="mt-1 text-xs text-gray-500">
              Hidden transactions stay in the list and account balance but leave spending, budgets and Top merchants. You can unhide any one later; the rule won&rsquo;t hide it again on the next sync.
            </p>
          </div>
          <div>
            <label className="flex items-center gap-2 text-sm text-gray-700">
              <input type="checkbox" checked={markReviewed} onChange={(e) => setMarkReviewed(e.target.checked)} />
              Also mark matching transactions reviewed
            </label>
            <p className="mt-1 text-xs text-gray-500">
              For merchants you trust, like rent or payroll: matches skip Needs review. You can send any one back to review later; the rule won&rsquo;t mark it again on the next sync.
            </p>
          </div>
          <div>
            <label htmlFor="rule-add-tags" className="mb-1 block text-xs font-medium text-gray-600">Also add tags (optional)</label>
            <input
              id="rule-add-tags"
              className={inputClass}
              value={tagsText}
              onChange={(e) => setTagsText(e.target.value)}
              placeholder="e.g. work travel, reimbursable"
              maxLength={MAX_TAG_LENGTH * 12}
              aria-describedby="rule-add-tags-help"
            />
            {tagsError && <span role="alert" className="mt-1 block text-xs text-red-600">{tagsError}</span>}
            {tagSuggestions.length > 0 && (
              <div className="mt-1 flex flex-wrap gap-1.5">
                {tagSuggestions.map((s) => (
                  <button
                    key={s.name}
                    type="button"
                    onClick={() => addSuggestion(s.name)}
                    aria-label={`Add tag ${s.name} to this rule`}
                    className="min-h-7 rounded-full border border-gray-300 px-2 text-xs text-gray-700 hover:bg-gray-50"
                  >
                    + {s.name}
                  </button>
                ))}
              </div>
            )}
            <p id="rule-add-tags-help" className="mt-1 text-xs text-gray-500">
              Separate tags with commas. Matches gain these tags; tags they already have stay as they are. You can remove a tag from any one later; the rule won&rsquo;t add it back on the next sync.
            </p>
          </div>
          <label className="flex items-center gap-2 text-sm text-gray-700">
            <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
            Active
          </label>
          <div aria-live="polite" className="rounded-md bg-gray-50 px-3 py-2 text-xs text-gray-700">
            {!isActive ? (
              'Inactive rules don\u2019t categorize anything.'
            ) : impactLoading ? (
              'Checking which transactions this would affect\u2026'
            ) : impactData ? (
              <>
                <p>
                  {impactData.matched === 0
                    ? 'No existing transactions would use this rule.'
                    : `Would categorize ${impactData.matched} of ${impactData.scanned} existing transactions (${impactData.would_change} would change).`}
                  {rename && impactData.would_rename > 0 &&
                    ` ${impactData.would_rename} would show merchant “${rename}”.`}
                  {hideMatches && impactData.would_hide > 0 &&
                    ` ${impactData.would_hide} would be hidden from spending and budgets.`}
                  {markReviewed && impactData.would_mark_reviewed > 0 &&
                    ` ${impactData.would_mark_reviewed} would be marked reviewed.`}
                  {ruleTags.length > 0 && impactData.would_tag > 0 &&
                    ` ${impactData.would_tag} would be tagged ${ruleTags.join(', ')}.`}
                  {impactData.skipped_manual > 0 && ` ${impactData.skipped_manual} manual picks are never overridden.`}
                </p>
                {impactData.samples.length > 0 && (
                  <ul className="mt-1 list-disc pl-4 text-gray-500">
                    {impactData.samples.map((s, i) => (
                      <li key={i} className="truncate">{s}</li>
                    ))}
                  </ul>
                )}
                <p className="mt-1 text-gray-500">Nothing changes until you save, then apply rules.</p>
              </>
            ) : current ? (
              'Couldn\u2019t check the impact right now \u2014 you can still save.'
            ) : (
              'Fill in pattern and category to see how many transactions this would affect.'
            )}
          </div>
        </div>
        <div className="flex shrink-0 justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">Cancel</button>
          <button
            type="button"
            onClick={submit}
            disabled={submitting}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : mode === 'create' ? 'Create' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

function Field({ label, error, children }: { label: string; error?: string | null; children: ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
      {error && <span className="mt-1 block text-xs text-red-600">{error}</span>}
    </label>
  )
}

function extractErr(err: unknown): { code: string | null; message: string } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.error) return { code: r.data.code ?? null, message: r.data.error }
  }
  if (err instanceof Error) return { code: null, message: err.message }
  return { code: null, message: 'request failed' }
}
