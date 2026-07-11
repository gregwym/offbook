import { beforeEach, describe, it } from 'vitest'
import { useAuthStore } from '../store/authStore'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { SignupPage } from './SignupPage'

describe('SignupPage smoke', () => {
  beforeEach(() => {
    resetStores()
    useAuthStore.setState({
      user: null,
      hydrated: true,
      setup: { bootstrapped: true, signup_mode: 'invite_only' },
      error: null,
    })
  })

  it('renders the invite-accept form without a stuck loading state or error banner', async () => {
    renderPage(<SignupPage />)
    await expectHealthySmoke()
  })
})
