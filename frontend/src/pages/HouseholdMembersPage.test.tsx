import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores, setHouseholdScope } from '../test/testUtils'
import { HouseholdMembersPage } from './HouseholdMembersPage'

describe('HouseholdMembersPage smoke', () => {
  beforeEach(() => {
    resetStores()
    setHouseholdScope(10)
  })

  it('renders the member list without a stuck loading state or error banner', async () => {
    renderPage(<HouseholdMembersPage />)
    await expectHealthySmoke()
  })
})
