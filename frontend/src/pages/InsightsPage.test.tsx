import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { InsightsPage } from './InsightsPage'

describe('InsightsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the 5 personal-scope bands without a stuck loading state or error banner', async () => {
    renderPage(<InsightsPage />)
    await expectHealthySmoke()
  })
})
