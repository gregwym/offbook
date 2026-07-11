import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { RulesPage } from './RulesPage'

describe('RulesPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the rules list without a stuck loading state or error banner', async () => {
    renderPage(<RulesPage />)
    await expectHealthySmoke()
  })
})
