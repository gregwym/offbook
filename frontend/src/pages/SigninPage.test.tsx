import { beforeEach, describe, it } from 'vitest'
import { useAuthStore } from '../store/authStore'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { SigninPage } from './SigninPage'

describe('SigninPage smoke', () => {
  beforeEach(() => {
    resetStores()
    // Signed-out + bootstrapped is the only state that actually renders the
    // form — otherwise the page redirects (see SigninPage.tsx's Navigate
    // guards).
    useAuthStore.setState({
      user: null,
      hydrated: true,
      setup: { bootstrapped: true, signup_mode: 'invite_only' },
      error: null,
    })
  })

  it('renders the signin form without a stuck loading state or error banner', async () => {
    renderPage(<SigninPage />)
    await expectHealthySmoke()
  })
})
