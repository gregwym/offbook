import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores, setHouseholdScope } from '../test/testUtils'
import { HouseholdSettingsPage } from './HouseholdSettingsPage'

describe('HouseholdSettingsPage smoke', () => {
  beforeEach(() => {
    resetStores()
    setHouseholdScope(10)
  })

  it('renders household settings without a stuck loading state or error banner', async () => {
    renderPage(<HouseholdSettingsPage />)
    await expectHealthySmoke()
  })
})
