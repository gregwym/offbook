import { beforeEach, describe, it } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { render } from '@testing-library/react'
import { expectHealthySmoke, resetStores } from '../test/testUtils'
import { ReconciliationPage } from './ReconciliationPage'

describe('ReconciliationPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the checkpoint history without a stuck loading state or error banner', async () => {
    render(
      <MemoryRouter initialEntries={['/accounts/1/reconciliation']}>
        <Routes>
          <Route path="/accounts/:id/reconciliation" element={<ReconciliationPage />} />
        </Routes>
      </MemoryRouter>,
    )
    await expectHealthySmoke()
  })
})
