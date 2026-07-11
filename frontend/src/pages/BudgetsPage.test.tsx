import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { BudgetsPage } from './BudgetsPage'

describe('BudgetsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the personal-scope budget list without a stuck loading state or error banner', async () => {
    renderPage(<BudgetsPage />)
    await expectHealthySmoke()
  })
})
