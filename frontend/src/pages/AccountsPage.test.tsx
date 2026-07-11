import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { AccountsPage } from './AccountsPage'

describe('AccountsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders without a stuck loading state or error banner', async () => {
    renderPage(<AccountsPage />)
    await expectHealthySmoke()
  })
})
