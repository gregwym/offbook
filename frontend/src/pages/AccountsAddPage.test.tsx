import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { AccountsAddPage } from './AccountsAddPage'

describe('AccountsAddPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the two-tile picker without a stuck loading state or error banner', async () => {
    renderPage(<AccountsAddPage />)
    await expectHealthySmoke()
  })
})
