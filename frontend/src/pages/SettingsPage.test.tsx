import { beforeEach, describe, it } from 'vitest'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { SettingsPage } from './SettingsPage'

describe('SettingsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders AI, price, and linked-institution sections without a stuck loading state or error banner', async () => {
    renderPage(<SettingsPage />)
    await expectHealthySmoke()
  })
})
