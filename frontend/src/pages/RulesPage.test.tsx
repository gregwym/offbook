import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import * as fx from '../test/fixtures'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { RulesPage } from './RulesPage'

const API = '/api/v1'

describe('RulesPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the rules list without a stuck loading state or error banner', async () => {
    renderPage(<RulesPage />)
    await expectHealthySmoke()
  })

  // #366: an AI verdict cache entry surfaces as a "promote to rule"
  // suggestion; submitting it creates a real rule and the suggestion drops
  // out of the list (nothing server-side tracks "promoted", so this is a
  // client-side hide keyed on merchant_key — see ADR-0022 §6).
  it('promotes an AI category suggestion into a real rule', async () => {
    server.use(
      http.get(`${API}/categorization-verdicts`, () =>
        HttpResponse.json({
          data: [
            {
              id: 1,
              user_id: 1,
              merchant_key: 'TRADER JOES',
              category_id: fx.categoryGroceries.id,
              confidence: 0.87,
              provider: 'claude',
              created_at: '2026-07-01T00:00:00Z',
              updated_at: '2026-07-01T00:00:00Z',
              category: fx.categoryGroceries,
            },
          ],
          total: 1,
        }),
      ),
      http.post(`${API}/categorization-rules`, () =>
        HttpResponse.json({
          data: {
            id: 999,
            user_id: 1,
            pattern: 'TRADER JOES',
            match_type: 'contains',
            category_id: fx.categoryGroceries.id,
            priority: 10,
            is_active: true,
            created_at: '2026-07-01T00:00:00Z',
            updated_at: '2026-07-01T00:00:00Z',
          },
        }),
      ),
    )

    renderPage(<RulesPage />)
    await expectHealthySmoke()

    expect(await screen.findByText('TRADER JOES')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /create rule/i }))

    // The modal seeds pattern from the merchant key — submit as-is. The
    // modal's submit button in create mode is exactly "Create".
    fireEvent.click(await screen.findByRole('button', { name: 'Create' }))

    await waitFor(() => {
      expect(screen.queryByText('TRADER JOES')).not.toBeInTheDocument()
    })
  })

  it('previews rule impact as a dry run, then applies on confirmation', async () => {
    const bodies: unknown[] = []
    server.use(
      http.post(`${API}/categorization-rules/apply`, async ({ request }) => {
        const body = (await request.json()) as { dry_run?: boolean }
        bodies.push(body)
        return HttpResponse.json({
          data: { scanned: 40, updated: 7, skipped_manual: 2, recategorized: 0, renamed: 7, hidden: 0, marked_reviewed: 0, tagged: 3, dry_run: !!body.dry_run },
        })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Preview changes' }))
    // A rename-and-tag rule reads as such, not as "re-categorized" (#552).
    expect(await screen.findByText(/Preview \(7 of 40 transactions\): 7 would be renamed, 3 tagged; 2 manual picks stay as they are\. Nothing has been changed\./)).toBeTruthy()
    expect(screen.queryByText(/re-categorized|change category/)).toBeNull()
    expect(bodies).toEqual([{ dry_run: true }])

    fireEvent.click(screen.getByRole('button', { name: 'Apply now' }))
    await waitFor(() => expect(screen.getByText('Updated 7 of 40 transactions: 7 renamed, 3 tagged; 2 manual picks stayed as they were.')).toBeTruthy())
    expect(bodies[1]).toEqual({})
    expect(screen.queryByRole('button', { name: 'Apply now' })).toBeNull()
  })

  // #565: a committed re-apply offers Undo; undoing reports what came back
  // and what was kept, and the offer goes away.
  it('undoes a re-apply and says what was restored and kept', async () => {
    const undone: string[] = []
    server.use(
      http.post(`${API}/categorization-rules/apply`, () =>
        HttpResponse.json({
          data: { scanned: 40, updated: 12, skipped_manual: 0, recategorized: 12, renamed: 0, hidden: 0, marked_reviewed: 0, tagged: 0, dry_run: false, run_id: 31 },
        }),
      ),
      http.post(`${API}/categorization-rules/apply-runs/:id/undo`, ({ params }) => {
        undone.push(String(params.id))
        return HttpResponse.json({
          data: { id: 31, scope: 'all', rows_changed: 12, created_at: '2026-10-21T12:00:00Z', undone_at: '2026-10-21T12:01:00Z', rows_restored: 11, rows_kept: 1 },
        })
      }),
    )
    renderPage(<RulesPage />)
    expect(screen.queryByRole('button', { name: 'Undo re-apply' })).toBeNull()
    fireEvent.click(await screen.findByRole('button', { name: 'Re-apply to all transactions' }))
    expect(await screen.findByText(/Last re-apply changed 12 transactions\./)).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Undo re-apply' }))
    expect(
      await screen.findByText('Undone: 11 transactions are back as they were; 1 transaction changed since then stays as it is.'),
    ).toBeTruthy()
    expect(undone).toEqual(['31'])
    expect(screen.queryByRole('button', { name: 'Undo re-apply' })).toBeNull()
  })

  it('offers undo for the last re-apply after a reload, and explains a stale undo', async () => {
    let latestCalls = 0
    server.use(
      http.get(`${API}/categorization-rules/apply-runs/latest`, () => {
        latestCalls++
        return HttpResponse.json({
          data:
            latestCalls === 1
              ? { id: 8, scope: 'all', rows_changed: 1, created_at: '2026-10-21T12:00:00Z' }
              : { id: 8, scope: 'all', rows_changed: 1, created_at: '2026-10-21T12:00:00Z', undone_at: '2026-10-21T12:05:00Z', rows_restored: 1, rows_kept: 0 },
        })
      }),
      http.post(`${API}/categorization-rules/apply-runs/:id/undo`, () =>
        HttpResponse.json({ error: 'only your latest rules run can be undone, once', code: 'RULE_APPLY_NOT_UNDOABLE' }, { status: 409 }),
      ),
    )
    renderPage(<RulesPage />)
    expect(await screen.findByText(/Last re-apply changed 1 transaction\./)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Undo re-apply' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('only the latest re-apply can be undone, once')
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Undo re-apply' })).toBeNull())
  })

  it('shows the draft rule impact in the form before saving', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({
          data: { scanned: 40, matched: 3, would_change: 2, would_recategorize: 2, would_rename: 0, would_hide: 0, would_mark_reviewed: 0, would_tag: 0, skipped_manual: 1, samples: ['WHOLEFDS #1', 'WHOLEFDS #2'] },
        })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'WHOLEFDS' } })
    expect(screen.getByText(/Fill in pattern and category/)).toBeTruthy()

    const select = screen.getAllByRole('combobox').find((el) => el.textContent?.includes('pick one'))!
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[2].value) } })

    expect(await screen.findByText(/Applies to 3 of 40 existing transactions: 2 would change category\. 1 manual pick stays as it is\./)).toBeTruthy()
    expect(screen.getByText('WHOLEFDS #1')).toBeTruthy()
    expect(bodies).toHaveLength(1)
    expect(bodies[0]).toMatchObject({ pattern: 'WHOLEFDS', match_type: 'contains' })
    expect(bodies[0]).not.toHaveProperty('rule_id')
  })

  // #534: a rule can also rename the merchant. The draft preview carries the
  // rename and says how many rows would show it; saving sends it; the list
  // shows it; clearing it on edit sends '' so the backend drops it.
  it('renames the merchant from a rule, previews it, and clears it on edit', async () => {
    const previews: Record<string, unknown>[] = []
    const creates: Record<string, unknown>[] = []
    const patches: Record<string, unknown>[] = []
    let stored = [...fx.rules]
    server.use(
      http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: stored, total: stored.length })),
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        previews.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({
          data: { scanned: 10, matched: 2, would_change: 2, would_recategorize: 0, would_rename: 2, would_hide: 0, would_mark_reviewed: 0, would_tag: 0, skipped_manual: 0, samples: ['AMZN MKTP US*2K4'] },
        })
      }),
      http.post(`${API}/categorization-rules`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        creates.push(body)
        const r = { ...fx.rule1, id: 2, pattern: 'AMZN', rename_merchant: 'Amazon' }
        stored = [...stored, r]
        return HttpResponse.json({ data: r }, { status: 201 })
      }),
      http.patch(`${API}/categorization-rules/:id`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        patches.push(body)
        stored = stored.map((r) => (r.id === 2 ? { ...r, rename_merchant: null } : r))
        return HttpResponse.json({ data: stored.find((r) => r.id === 2) })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'AMZN' } })
    const select = screen.getAllByRole('combobox').find((el) => el.textContent?.includes('pick one'))!
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[2].value) } })
    fireEvent.change(screen.getByLabelText(/Rename merchant to/), { target: { value: '  Amazon ' } })
    // Run order lives only in the list (#552): no Priority field, and the
    // form says where a new rule lands.
    expect(screen.queryByLabelText(/Priority/)).toBeNull()
    expect(screen.getByText(/^New rules run first; reorder them on the Rules page\./)).toBeTruthy()

    expect(await screen.findByText(/2 would be renamed\. Renamed ones show merchant “Amazon”/)).toBeTruthy()
    expect(previews.at(-1)).toMatchObject({ pattern: 'AMZN', rename_merchant: 'Amazon' })

    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Renames to Amazon')).toBeTruthy()
    expect(creates[0]).toMatchObject({ pattern: 'AMZN', rename_merchant: 'Amazon', priority: 10 })

    const row = screen.getByText('Renames to Amazon').closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: /edit/i }))
    const input = screen.getByLabelText(/Rename merchant to/) as HTMLInputElement
    expect(input.value).toBe('Amazon')
    fireEvent.change(input, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByText('Renames to Amazon')).toBeNull())
    expect(patches[0]).toMatchObject({ rename_merchant: '' })
    // Editing never moves the rule.
    expect(patches[0]).not.toHaveProperty('priority')
  })

  // #536: a rule can also hide its matches. The draft preview carries the
  // flag and says how many rows would be hidden; saving sends it; the list
  // marks the rule; unticking on edit sends false.
  it('hides matching transactions from a rule, previews it, and turns it off on edit', async () => {
    const previews: Record<string, unknown>[] = []
    const creates: Record<string, unknown>[] = []
    const patches: Record<string, unknown>[] = []
    let stored = [...fx.rules]
    server.use(
      http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: stored, total: stored.length })),
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        previews.push(body)
        return HttpResponse.json({
          data: { scanned: 10, matched: 3, would_change: 3, would_recategorize: 0, would_rename: 0, would_hide: body.hide_matches ? 3 : 0, would_mark_reviewed: 0, would_tag: 0, skipped_manual: 0, samples: ['CASH SWEEP'] },
        })
      }),
      http.post(`${API}/categorization-rules`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        creates.push(body)
        const r = { ...fx.rule1, id: 3, pattern: 'SWEEP', hide_matches: true }
        stored = [...stored, r]
        return HttpResponse.json({ data: r }, { status: 201 })
      }),
      http.patch(`${API}/categorization-rules/:id`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        patches.push(body)
        stored = stored.map((r) => (r.id === 3 ? { ...r, hide_matches: false } : r))
        return HttpResponse.json({ data: stored.find((r) => r.id === 3) })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'SWEEP' } })
    const select = screen.getAllByRole('combobox').find((el) => el.textContent?.includes('pick one'))!
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[2].value) } })
    fireEvent.click(screen.getByLabelText(/Also hide matching transactions/))

    expect(await screen.findByText(/3 would be hidden\. Hidden ones leave spending and budgets/)).toBeTruthy()
    expect(previews.at(-1)).toMatchObject({ pattern: 'SWEEP', hide_matches: true })

    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Hides matches')).toBeTruthy()
    expect(creates[0]).toMatchObject({ pattern: 'SWEEP', hide_matches: true })

    const row = screen.getByText('Hides matches').closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: /edit/i }))
    const box = screen.getByLabelText(/Also hide matching transactions/) as HTMLInputElement
    expect(box.checked).toBe(true)
    fireEvent.click(box)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByText('Hides matches')).toBeNull())
    expect(patches[0]).toMatchObject({ hide_matches: false })
  })

  // #538: a rule can also mark its matches reviewed. The draft preview
  // carries the flag and counts the rows; saving sends it; the list marks the
  // rule; unticking on edit sends false.
  it('marks matching transactions reviewed from a rule, previews it, and turns it off on edit', async () => {
    const previews: Record<string, unknown>[] = []
    const creates: Record<string, unknown>[] = []
    const patches: Record<string, unknown>[] = []
    let stored = [...fx.rules]
    server.use(
      http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: stored, total: stored.length })),
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        previews.push(body)
        return HttpResponse.json({
          data: { scanned: 10, matched: 2, would_change: 2, would_recategorize: 0, would_rename: 0, would_hide: 0, would_mark_reviewed: body.mark_reviewed ? 2 : 0, would_tag: 0, skipped_manual: 0, samples: ['ACME PAYROLL'] },
        })
      }),
      http.post(`${API}/categorization-rules`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        creates.push(body)
        const r = { ...fx.rule1, id: 4, pattern: 'PAYROLL', mark_reviewed: true }
        stored = [...stored, r]
        return HttpResponse.json({ data: r }, { status: 201 })
      }),
      http.patch(`${API}/categorization-rules/:id`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        patches.push(body)
        stored = stored.map((r) => (r.id === 4 ? { ...r, mark_reviewed: false } : r))
        return HttpResponse.json({ data: stored.find((r) => r.id === 4) })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'PAYROLL' } })
    const select = screen.getAllByRole('combobox').find((el) => el.textContent?.includes('pick one'))!
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[2].value) } })
    fireEvent.click(screen.getByLabelText(/Also mark matching transactions reviewed/))

    expect(await screen.findByText(/2 would be marked reviewed/)).toBeTruthy()
    expect(previews.at(-1)).toMatchObject({ pattern: 'PAYROLL', mark_reviewed: true })

    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Marks reviewed')).toBeTruthy()
    expect(creates[0]).toMatchObject({ pattern: 'PAYROLL', mark_reviewed: true })

    const row = screen.getByText('Marks reviewed').closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: /edit/i }))
    const box = screen.getByLabelText(/Also mark matching transactions reviewed/) as HTMLInputElement
    expect(box.checked).toBe(true)
    fireEvent.click(box)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByText('Marks reviewed')).toBeNull())
    expect(patches[0]).toMatchObject({ mark_reviewed: false })
  })

  // #540: rules run top to bottom; moving one sends the full new order and
  // a rejected reorder puts the list back to the server order.
  describe('run order', () => {
    const amazon = { ...fx.rules[0], id: 11, pattern: 'AMAZON', priority: 20 }
    const prime = { ...fx.rules[0], id: 12, pattern: 'AMAZON PRIME', priority: 10 }
    const order = () =>
      screen.getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('cell')[1].textContent)

    it('moves a rule up and saves the whole order', async () => {
      const bodies: unknown[] = []
      server.use(
        http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: [amazon, prime], total: 2 })),
        http.post(`${API}/categorization-rules/reorder`, async ({ request }) => {
          bodies.push(await request.json())
          return HttpResponse.json({ data: [{ ...prime, priority: 20 }, { ...amazon, priority: 10 }], total: 2 })
        }),
      )
      renderPage(<RulesPage />)
      expect(await screen.findByText('AMAZON PRIME')).toBeTruthy()
      expect(screen.getByText(/the first match wins/)).toBeTruthy()
      expect(order()).toEqual(['AMAZON', 'AMAZON PRIME'])
      expect((screen.getByRole('button', { name: 'Move "AMAZON" up' }) as HTMLButtonElement).disabled).toBe(true)
      expect((screen.getByRole('button', { name: 'Move "AMAZON PRIME" down' }) as HTMLButtonElement).disabled).toBe(true)

      fireEvent.click(screen.getByRole('button', { name: 'Move "AMAZON PRIME" up' }))
      await waitFor(() => expect(bodies).toEqual([{ rule_ids: [12, 11] }]))
      expect(order()).toEqual(['AMAZON PRIME', 'AMAZON'])
      expect(screen.queryByText(/rule order must/)).toBeNull()
    })

    it('shows the error and restores the server order when a reorder fails', async () => {
      server.use(
        http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: [amazon, prime], total: 2 })),
        http.post(`${API}/categorization-rules/reorder`, () =>
          HttpResponse.json(
            { error: 'rule order must list each of your rules exactly once', code: 'INVALID_RULE_ORDER' },
            { status: 400 },
          ),
        ),
      )
      renderPage(<RulesPage />)
      expect(await screen.findByText('AMAZON PRIME')).toBeTruthy()
      fireEvent.click(screen.getByRole('button', { name: 'Move "AMAZON" down' }))
      expect(await screen.findByText(/rule order must list each of your rules exactly once/)).toBeTruthy()
      expect(order()).toEqual(['AMAZON', 'AMAZON PRIME'])
    })
  })
  // #546: a rule can also add tags. Typed tags are normalized (dedupe, the
  // user's spelling), a suggestion chip appends one, the preview counts the
  // rows it would tag, the list shows them, and clearing on edit sends [].
  it('adds tags from a rule, previews them, and clears them on edit', async () => {
    const previews: Record<string, unknown>[] = []
    const creates: Record<string, unknown>[] = []
    const patches: Record<string, unknown>[] = []
    let stored = [...fx.rules]
    server.use(
      http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: stored, total: stored.length })),
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        previews.push(body)
        return HttpResponse.json({
          data: { scanned: 10, matched: 3, would_change: 3, would_recategorize: 0, would_rename: 0, would_hide: 0, would_mark_reviewed: 0, would_tag: body.add_tags ? 3 : 0, skipped_manual: 0, samples: ['UBER TRIP'] },
        })
      }),
      http.post(`${API}/categorization-rules`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        creates.push(body)
        const r = { ...fx.rule1, id: 5, pattern: 'UBER', add_tags: body.add_tags as string[] }
        stored = [...stored, r]
        return HttpResponse.json({ data: r }, { status: 201 })
      }),
      http.patch(`${API}/categorization-rules/:id`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        patches.push(body)
        stored = stored.map((r) => (r.id === 5 ? { ...r, add_tags: body.add_tags as string[] } : r))
        return HttpResponse.json({ data: stored.find((r) => r.id === 5) })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'UBER' } })
    const select = screen.getAllByRole('combobox').find((el) => el.textContent?.includes('pick one'))!
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[2].value) } })
    fireEvent.change(screen.getByLabelText(/Also add tags/), { target: { value: 'work trip, reimbursable, Reimbursable' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Add tag vacation to this rule' }))

    expect(await screen.findByText(/3 would be tagged\. Tags: Work trip, reimbursable, vacation/)).toBeTruthy()
    expect(previews.at(-1)).toMatchObject({ pattern: 'UBER', add_tags: ['Work trip', 'reimbursable', 'vacation'] })

    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Adds tags: Work trip, reimbursable, vacation')).toBeTruthy()
    expect(creates[0]).toMatchObject({ pattern: 'UBER', add_tags: ['Work trip', 'reimbursable', 'vacation'] })

    const row = screen.getByText(/Adds tags:/).closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: /edit/i }))
    const input = screen.getByLabelText(/Also add tags/) as HTMLInputElement
    expect(input.value).toBe('Work trip, reimbursable, vacation')
    fireEvent.change(input, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByText(/Adds tags:/)).toBeNull())
    expect(patches[0]).toMatchObject({ add_tags: [] })
  })

  // #553: "Keep current category" makes a tag-only rule: no category_id is
  // sent, the list says "Category unchanged", a rule with no other action is
  // blocked, and editing back to keep sends clear_category_id.
  it('creates a tag-only rule that keeps the category, then clears a category on edit', async () => {
    const previews: Record<string, unknown>[] = []
    const creates: Record<string, unknown>[] = []
    const patches: Record<string, unknown>[] = []
    let stored = [...fx.rules]
    server.use(
      http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: stored, total: stored.length })),
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        previews.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({
          data: { scanned: 10, matched: 2, would_change: 2, would_recategorize: 0, would_rename: 0, would_hide: 0, would_mark_reviewed: 0, would_tag: 2, skipped_manual: 0, samples: ['UBER TRIP'] },
        })
      }),
      http.post(`${API}/categorization-rules`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        creates.push(body)
        const r = { ...fx.rule1, id: 6, pattern: 'UBER', category_id: null, add_tags: body.add_tags as string[] }
        stored = [...stored, r]
        return HttpResponse.json({ data: r }, { status: 201 })
      }),
      http.patch(`${API}/categorization-rules/:id`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        patches.push(body)
        stored = stored.map((r) => (r.id === fx.rule1.id ? { ...r, category_id: null, mark_reviewed: true } : r))
        return HttpResponse.json({ data: stored.find((r) => r.id === fx.rule1.id) })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'UBER' } })
    expect(screen.getByText(/This rule stops later rules from running on the same transaction/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Category'), { target: { value: 'keep' } })

    // Keep category alone does nothing: no preview, and saving is blocked.
    expect(screen.getByText(/Add a rename, tags, hide or mark reviewed to see/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText(/This rule would change nothing/)).toBeTruthy()
    expect(creates).toHaveLength(0)

    fireEvent.change(screen.getByLabelText(/Also add tags/), { target: { value: 'reimbursable' } })
    expect(screen.queryByText(/This rule would change nothing/)).toBeNull()
    expect(await screen.findByText(/2 would be tagged\. Tags: reimbursable\. Categories stay as they are\./)).toBeTruthy()
    expect(previews.at(-1)).not.toHaveProperty('category_id')

    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    const row = (await screen.findByText('Adds tags: reimbursable')).closest('tr')!
    expect(within(row).getByText('Category unchanged')).toBeTruthy()
    expect(creates[0]).toMatchObject({ pattern: 'UBER', add_tags: ['reimbursable'] })
    expect(creates[0]).not.toHaveProperty('category_id')

    // Editing a categorizing rule to keep the category sends clear_category_id.
    const first = screen.getByText(fx.rule1.pattern).closest('tr')!
    fireEvent.click(within(first).getByRole('button', { name: /edit/i }))
    fireEvent.change(screen.getByLabelText('Category'), { target: { value: 'keep' } })
    fireEvent.click(screen.getByLabelText(/Also mark matching transactions reviewed/))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(patches).toHaveLength(1))
    expect(patches[0]).toMatchObject({ clear_category_id: true, mark_reviewed: true })
    expect(patches[0]).not.toHaveProperty('category_id')
    await waitFor(() => expect(within(screen.getByText(fx.rule1.pattern).closest('tr')!).getByText('Category unchanged')).toBeTruthy())
  })

  it('blocks saving a rule tag that is too long', async () => {
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByLabelText(/Also add tags/), { target: { value: 'x'.repeat(41) } })
    expect(await screen.findByText(/Keep each tag to 40 characters or fewer/)).toBeTruthy()
  })
  // #550: account + amount-band conditions round-trip through the form, the
  // draft preview, the rules list wording, and clear_* on edit.
  it('narrows a rule to an account and an amount band, then clears the band on edit', async () => {
    const previews: Record<string, unknown>[] = []
    const creates: Record<string, unknown>[] = []
    const patches: Record<string, unknown>[] = []
    let stored = [...fx.rules]
    server.use(
      http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: stored, total: stored.length })),
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        previews.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({
          data: { scanned: 10, matched: 1, would_change: 1, would_recategorize: 0, would_rename: 0, would_hide: 0, would_mark_reviewed: 0, would_tag: 0, skipped_manual: 0, samples: ['AMZN Mktp US'] },
        })
      }),
      http.post(`${API}/categorization-rules`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        creates.push(body)
        const r = {
          ...fx.rule1, id: 6, pattern: 'AMZN',
          account_id: body.account_id as number,
          amount_min: '10.000000000000000000', amount_max: '20.000000000000000000',
        }
        stored = [...stored, r]
        return HttpResponse.json({ data: r }, { status: 201 })
      }),
      http.patch(`${API}/categorization-rules/:id`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        patches.push(body)
        stored = stored.map((r) => (r.id === 6 ? { ...r, amount_min: null, amount_max: null } : r))
        return HttpResponse.json({ data: stored.find((r) => r.id === 6) })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'AMZN' } })
    const select = screen.getAllByRole('combobox').find((el) => el.textContent?.includes('pick one'))!
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[2].value) } })
    const accountSelect = screen.getByLabelText('Account') as HTMLSelectElement
    await waitFor(() => expect(accountSelect.options.length).toBeGreaterThan(1))
    fireEvent.change(accountSelect, { target: { value: String(fx.account1.id) } })

    // An inverted band is caught before anything is sent.
    fireEvent.change(screen.getByLabelText('Lowest amount'), { target: { value: '30' } })
    fireEvent.change(screen.getByLabelText('Highest amount'), { target: { value: '$20' } })
    expect(await screen.findByText(/lower amount must not be more/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Lowest amount'), { target: { value: '10' } })

    expect(await screen.findByText(/Applies to 1 of 10/)).toBeTruthy()
    expect(previews.at(-1)).toMatchObject({ pattern: 'AMZN', account_id: fx.account1.id, amount_min: '10', amount_max: '20' })

    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText(`Only in ${fx.account1.name}`)).toBeTruthy()
    expect(screen.getByText(`Only in ${fx.account1.name}`).closest('td')!.textContent).toMatch(/Amount 10\.00 USD to 20\.00 USD/)
    expect(creates[0]).toMatchObject({ account_id: fx.account1.id, amount_min: '10', amount_max: '20' })

    const row = screen.getByText(`Only in ${fx.account1.name}`).closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: /edit/i }))
    const low = screen.getByLabelText('Lowest amount') as HTMLInputElement
    expect(low.value).toBe('10')
    fireEvent.change(low, { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Highest amount'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByText(`Only in ${fx.account1.name}`).closest('td')!.textContent).not.toMatch(/Amount/))
    expect(patches[0]).toMatchObject({ account_id: fx.account1.id, clear_amount_min: true, clear_amount_max: true })
  })
})

describe('RulesPage empty state (#627)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('hides bulk actions and offers to create the first rule', async () => {
    server.use(http.get(`${API}/categorization-rules`, () => HttpResponse.json({ data: [], total: 0 })))
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Create your first rule' }))
    expect(screen.queryByRole('button', { name: 'Preview changes' })).toBeNull()
    expect(screen.queryByRole('button', { name: /re-apply to all transactions/i })).toBeNull()
    expect(await screen.findByRole('dialog', { name: 'New rule' })).toBeTruthy()
  })
})
