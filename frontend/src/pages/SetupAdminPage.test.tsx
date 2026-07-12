import { beforeEach, describe, it } from 'vitest'
import { useAuthStore } from '../store/authStore'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { SetupAdminPage } from './SetupAdminPage'

describe('SetupAdminPage smoke', () => {
  beforeEach(() => {
    resetStores()
    // Pre-bootstrap is the only state that renders the form — once
    // bootstrapped, the page redirects away (see SetupAdminPage.tsx).
    useAuthStore.setState({
      user: null,
      hydrated: true,
      setup: { bootstrapped: false, signup_mode: null },
      error: null,
    })
  })

  it('renders the first-boot admin form without a stuck loading state or error banner', async () => {
    renderPage(<SetupAdminPage />)
    await expectHealthySmoke()
  })
})
