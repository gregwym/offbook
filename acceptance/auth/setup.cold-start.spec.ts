import { expect, test } from '@playwright/test'
import { personaPassword } from '../fixtures/env.mjs'
import { request, resetToColdStart, waitForBackend } from '../fixtures/state.mjs'

test.describe('auth and setup cold-start acceptance', () => {
  test.beforeAll(async () => {
    await waitForBackend()
    await resetToColdStart()
  })

  test('setup admin, invite-only signup, and household invite flow work through API product paths', async () => {
    const adminEmail = 'qa+cold-admin@offbook.local'
    const memberEmail = 'qa+cold-member@offbook.local'

    const statusBefore = await request('/setup/status')
    expect(statusBefore.res.status()).toBe(200)
    expect(statusBefore.json.data.bootstrapped).toBe(false)

    const setup = await request('/setup/admin', {
      method: 'POST',
      body: {
        email: adminEmail,
        password: personaPassword(adminEmail),
        signup_mode: 'invite_only',
      },
    })
    expect(setup.res.status()).toBe(201)
    expect(setup.cookie).toBeTruthy()

    const closedSignup = await request('/auth/signup', {
      method: 'POST',
      body: {
        email: 'qa+cold-closed@offbook.local',
        password: personaPassword('qa+cold-closed@offbook.local'),
      },
    })
    expect(closedSignup.res.status()).toBe(403)
    expect(closedSignup.json.code).toBe('SIGNUP_CLOSED')

    const household = await request('/households', {
      method: 'POST',
      cookie: setup.cookie,
      body: { name: 'Cold Start Household' },
    })
    expect(household.res.status()).toBe(201)
    const householdID = household.json.data.id

    const invite = await request(`/households/${householdID}/invites`, {
      method: 'POST',
      cookie: setup.cookie,
      body: { role: 'contributor' },
    })
    expect(invite.res.status()).toBe(201)
    expect(invite.json.data.token).toBeTruthy()

    const signup = await request('/auth/signup-with-invite', {
      method: 'POST',
      body: {
        email: memberEmail,
        password: personaPassword(memberEmail),
        invite_token: invite.json.data.token,
      },
    })
    expect(signup.res.status()).toBe(201)
    expect(signup.cookie).toBeTruthy()

    const members = await request(`/households/${householdID}/members`, {
      cookie: setup.cookie,
    })
    expect(members.res.status()).toBe(200)
    expect(members.json.data.active.map((member: { user_id: number }) => member.user_id)).toContain(signup.json.data.id)
  })
})
