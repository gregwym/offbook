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
          data: { scanned: 40, updated: 7, skipped_manual: 2, dry_run: !!body.dry_run },
        })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Preview changes' }))
    expect(await screen.findByText(/7 of 40 transactions would be re-categorized/)).toBeTruthy()
    expect(bodies).toEqual([{ dry_run: true }])

    fireEvent.click(screen.getByRole('button', { name: 'Apply now' }))
    await waitFor(() => expect(screen.getByText(/Scanned 40, updated 7/)).toBeTruthy())
    expect(bodies[1]).toEqual({})
    expect(screen.queryByRole('button', { name: 'Apply now' })).toBeNull()
  })

  it('shows the draft rule impact in the form before saving', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post(`${API}/categorization-rules/preview`, async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({
          data: { scanned: 40, matched: 3, would_change: 2, would_rename: 0, skipped_manual: 1, samples: ['WHOLEFDS #1', 'WHOLEFDS #2'] },
        })
      }),
    )
    renderPage(<RulesPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new rule/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g. WHOLEFDS'), { target: { value: 'WHOLEFDS' } })
    expect(screen.getByText(/Fill in pattern and category/)).toBeTruthy()

    const select = screen.getAllByRole('combobox').find((el) => el.textContent?.includes('pick one'))!
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[1].value) } })

    expect(await screen.findByText(/Would categorize 3 of 40 existing transactions \(2 would change\)/)).toBeTruthy()
    expect(screen.getByText(/1 manual picks are never overridden/)).toBeTruthy()
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
          data: { scanned: 10, matched: 2, would_change: 2, would_rename: 2, skipped_manual: 0, samples: ['AMZN MKTP US*2K4'] },
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
    fireEvent.change(select, { target: { value: String((select as HTMLSelectElement).options[1].value) } })
    fireEvent.change(screen.getByLabelText(/Rename merchant to/), { target: { value: '  Amazon ' } })

    expect(await screen.findByText(/2 would show merchant “Amazon”/)).toBeTruthy()
    expect(previews.at(-1)).toMatchObject({ pattern: 'AMZN', rename_merchant: 'Amazon' })

    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Renames to Amazon')).toBeTruthy()
    expect(creates[0]).toMatchObject({ pattern: 'AMZN', rename_merchant: 'Amazon' })

    const row = screen.getByText('Renames to Amazon').closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: /edit/i }))
    const input = screen.getByLabelText(/Rename merchant to/) as HTMLInputElement
    expect(input.value).toBe('Amazon')
    fireEvent.change(input, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByText('Renames to Amazon')).toBeNull())
    expect(patches[0]).toMatchObject({ rename_merchant: '' })
  })
})
