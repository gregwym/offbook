import { fireEvent, screen, waitFor } from '@testing-library/react'
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
})
