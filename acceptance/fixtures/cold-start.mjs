#!/usr/bin/env node
import { acceptanceAPIURL } from './env.mjs'
import { assertQARole, resetToColdStart, waitForBackend } from './state.mjs'

assertQARole()
await waitForBackend(acceptanceAPIURL())
await resetToColdStart()

console.log('QA database reset to pre-/setup/admin cold-start state')
