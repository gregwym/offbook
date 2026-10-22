import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import * as fx from '../test/fixtures'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { TransactionsPage } from '../pages/TransactionsPage'

const API = '/api/v1'
const PETS = { ...fx.categoryGroceries, id: 77, name: 'Pets', slug: 'pets', is_system: false, user_id: 1 }

// #581: a category picker offers "+ New category…"; the dialog creates the
// category and the originating row is categorised with it in the same step.
describe('New category from the transaction row picker (#581)', () => {
  beforeEach(() => {
    resetStores()
  })

  function install() {
    const created: unknown[] = []
    const patched: unknown[] = []
    const row = { ...fx.transaction1, id: 91, description: 'VET CLINIC 22', merchant_name: null, description_clean: null, category_id: null }
    server.use(
      http.get(`${API}/transactions`, () => HttpResponse.json({ data: [row], total: 1 })),
      http.post(`${API}/categories`, async ({ request }) => {
        const body = (await request.json()) as { name: string }
        created.push(body)
        if (body.name === 'Groceries') {
          return HttpResponse.json({ error: 'name taken', code: 'CATEGORY_NAME_TAKEN' }, { status: 409 })
        }
        return HttpResponse.json({ data: { ...PETS, name: body.name } }, { status: 201 })
      }),
      http.patch(`${API}/transactions/91`, async ({ request }) => {
        patched.push(await request.json())
        return HttpResponse.json({ data: { ...row, category_id: PETS.id, categorization_method: 'manual' } })
      }),
    )
    return { created, patched }
  }

  async function pickNew() {
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    const select = (await screen.findAllByRole('combobox', { name: 'Category for VET CLINIC 22' }))[0] as HTMLSelectElement
    // No focus first: a programmatic pick (or a test driver) needn't focus
    // the select, and focus must still come back to it.
    fireEvent.change(select, { target: { value: '__new_category__' } })
    return select
  }

  it('creates the category and categorises the row with it', async () => {
    const { created, patched } = install()
    const select = await pickNew()
    const dlg = await screen.findByRole('dialog', { name: 'New category' })
    // The picker didn't jump to the sentinel; the row is still uncategorised.
    expect(select.value).toBe('')
    expect(within(dlg).getByLabelText('Name')).toHaveFocus()
    expect(within(dlg).getByRole('link', { name: 'Manage categories' })).toHaveAttribute('href', '/settings#categories')

    // Duplicate name: a sentence in the dialog, nothing sent to the row.
    fireEvent.change(within(dlg).getByLabelText('Name'), { target: { value: 'Groceries' } })
    fireEvent.click(within(dlg).getByRole('button', { name: 'Create' }))
    expect(await within(dlg).findByRole('alert')).toHaveTextContent('“Groceries” already exists. Pick a different name.')
    expect(patched).toHaveLength(0)

    fireEvent.change(within(dlg).getByLabelText('Name'), { target: { value: 'Pets' } })
    fireEvent.click(within(dlg).getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(patched).toEqual([{ category_id: PETS.id }]))
    expect(created).toEqual([{ name: 'Groceries', parent_id: null }, { name: 'Pets', parent_id: null }])
    expect(screen.queryByRole('dialog', { name: 'New category' })).toBeNull()
    await waitFor(() => expect(select.value).toBe(String(PETS.id)))
    expect(select).toHaveFocus()
  })

  it('cancel and Escape send nothing and keep the previous value', async () => {
    const { created, patched } = install()
    const select = await pickNew()
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'New category' })).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog', { name: 'New category' })).toBeNull()
    expect(select).toHaveFocus()

    fireEvent.change(select, { target: { value: '__new_category__' } })
    await screen.findByRole('dialog', { name: 'New category' })
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'New category' })).toBeNull()
    expect(select.value).toBe('')
    expect(created).toHaveLength(0)
    expect(patched).toHaveLength(0)
  })
})
