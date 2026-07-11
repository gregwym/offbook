import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { TransactionsPage } from './TransactionsPage'

describe('TransactionsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the transaction list without a stuck loading state or error banner', async () => {
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
  })
})
