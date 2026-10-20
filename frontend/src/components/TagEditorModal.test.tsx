import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import * as fx from '../test/fixtures'
import { TagEditorModal } from './TagEditorModal'
import { mergeTags } from '../lib/tags'

const tx = { ...fx.transaction1, tags: ['vacation'] }
const suggestions = [{ name: 'vacation', count: 2 }, { name: 'Work trip', count: 1 }]

function setup() {
  const onSaved = vi.fn()
  const onClose = vi.fn()
  render(<TagEditorModal tx={tx} suggestions={suggestions} onClose={onClose} onSaved={onSaved} />)
  return { onSaved, onClose }
}

describe('mergeTags (#544)', () => {
  it('normalizes like the server and reuses the existing spelling', () => {
    expect(mergeTags(['a'], '  work   TRIP , ,A, new', suggestions)).toEqual({ next: ['a', 'Work trip', 'new'], error: null })
  })
  it('rejects an over-long tag and an 11th tag', () => {
    expect(mergeTags([], 'x'.repeat(41), []).error).toMatch(/40 characters/)
    const ten = Array.from({ length: 10 }, (_, i) => `t${i}`)
    expect(mergeTags(ten, 'eleven', []).error).toMatch(/up to 10 tags/)
  })
})

describe('TagEditorModal (#544)', () => {
  it('adds a suggestion and a typed tag, removes one, and saves the list', async () => {
    let body: unknown
    server.use(
      http.patch('/api/v1/transactions/:id', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ data: tx })
      }),
    )
    const { onSaved, onClose } = setup()
    const current = screen.getByRole('list', { name: 'Current tags' })
    expect(within(current).getByText('vacation')).toBeInTheDocument()
    // Already-applied tags aren't suggested again.
    expect(screen.queryByRole('button', { name: 'Add tag vacation' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add tag Work trip' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove tag vacation' }))
    // Typed but not "Add"-ed still counts on Save.
    fireEvent.change(screen.getByLabelText('New tag'), { target: { value: 'reimbursable' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save tags' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(onClose).toHaveBeenCalled()
    expect(body).toEqual({ tags: ['Work trip', 'reimbursable'] })
  })

  it('keeps the dialog open with the server error when saving fails', async () => {
    server.use(
      http.patch('/api/v1/transactions/:id', () =>
        HttpResponse.json({ error: 'invalid tags: at most 10 tags per transaction', code: 'INVALID_TAGS' }, { status: 400 }),
      ),
    )
    const { onSaved, onClose } = setup()
    fireEvent.click(screen.getByRole('button', { name: 'Save tags' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('at most 10 tags')
    expect(onSaved).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('shows the length limit without calling the server', () => {
    setup()
    fireEvent.change(screen.getByLabelText('New tag'), { target: { value: 'x'.repeat(41) } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(screen.getByRole('alert')).toHaveTextContent('40 characters')
  })
})
