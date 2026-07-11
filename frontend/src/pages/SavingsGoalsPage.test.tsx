import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { SavingsGoalsPage } from './SavingsGoalsPage'

describe('SavingsGoalsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the personal-scope goal list without a stuck loading state or error banner', async () => {
    renderPage(<SavingsGoalsPage />)
    await expectHealthySmoke()
  })
})
