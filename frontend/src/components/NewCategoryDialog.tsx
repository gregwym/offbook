import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { categoryErr } from '../lib/categoryErr'
import { useCategoriesStore } from '../store/categoriesStore'
import type { Category } from '../types/category'
import { ModalShell } from './ModalShell'

// NEW_CATEGORY is the value of the "+ New category…" option a category picker
// appends (#581). Choosing it opens NewCategoryDialog instead of selecting.
export const NEW_CATEGORY = '__new_category__'

export function NewCategoryOption() {
  return <option value={NEW_CATEGORY}>+ New category…</option>
}

type Props = {
  onCreated: (c: Category) => void
  onCancel: () => void
  // The picker that opened the dialog; focus goes back to it on close.
  // Defaults to whatever had focus when the dialog mounted.
  returnFocusTo?: HTMLElement | null
}

// NewCategoryDialog creates one of the user's own categories from wherever
// they are picking one (#581) — a transaction row, the bulk bar, a rule or a
// budget — so they don't have to know it lives in Settings. The store adds
// it to every picker at once; the caller selects it in the control that
// opened the dialog. Focus returns to that control when the dialog closes.
export function NewCategoryDialog({ onCreated, onCancel, returnFocusTo }: Props) {
  const { categories, create } = useCategoriesStore()
  const [name, setName] = useState('')
  const [parentID, setParentID] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const opener = useRef<HTMLElement | null>(
    returnFocusTo ?? (typeof document !== 'undefined' ? (document.activeElement as HTMLElement | null) : null),
  )
  // Only top-level categories can be a group; the taxonomy is two levels.
  const groups = categories.filter((c) => c.parent_id == null)

  useEffect(() => {
    const el = opener.current
    return () => {
      if (el && el.isConnected) el.focus()
    }
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onCancel() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onCancel])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setError('Enter a name for the category.')
      return
    }
    setSaving(true)
    setError(null)
    try {
      const c = await create({ name: trimmed, parent_id: parentID ? Number(parentID) : null })
      onCreated(c)
    } catch (err) {
      setError(categoryErr(err, trimmed))
      setSaving(false)
    }
  }

  return (
    <ModalShell sheet size="md" aria-labelledby="new-category-title">
      <form className="contents" onSubmit={(e) => void submit(e)}>
        <div className="border-b border-gray-200 px-5 py-3">
          <div id="new-category-title" className="text-lg font-semibold text-gray-900">New category</div>
          <div className="text-sm text-gray-600">It shows up everywhere you pick a category.</div>
        </div>
        <div className="space-y-4 px-5 py-4">
          <div>
            <label htmlFor="new-category-name" className="text-sm font-medium text-gray-700">Name</label>
            <input
              id="new-category-name"
              autoFocus
              value={name}
              maxLength={60}
              onChange={(e) => { setName(e.target.value); if (error) setError(null) }}
              placeholder="e.g. Pets"
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? 'new-category-error' : undefined}
              className="mt-1 block min-h-10 w-full rounded border border-gray-300 px-2 text-sm"
            />
          </div>
          <div>
            <label htmlFor="new-category-parent" className="text-sm font-medium text-gray-700">Group under (optional)</label>
            <select
              id="new-category-parent"
              value={parentID}
              onChange={(e) => setParentID(e.target.value)}
              className="mt-1 block min-h-10 w-full rounded border border-gray-300 bg-white px-2 text-sm"
            >
              <option value="">None</option>
              {groups.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </div>
          {error && <p id="new-category-error" role="alert" className="text-sm text-red-700">{error}</p>}
          <p className="text-xs text-gray-500">
            Rename, merge or delete your categories in{' '}
            <Link to="/settings#categories" className="text-indigo-700 underline">Manage categories</Link>.
          </p>
        </div>
        <div className="flex flex-wrap justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onCancel} className="min-h-10 rounded border border-gray-300 px-4 text-sm">Cancel</button>
          <button
            type="submit"
            disabled={saving}
            className="min-h-10 rounded bg-indigo-600 px-4 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {saving ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </ModalShell>
  )
}
