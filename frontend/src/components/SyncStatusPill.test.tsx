// Coverage for #365: the pill must render all six last_sync_status values
// (the type only used to declare four; ok_with_errors/reauth_required were
// silently falling through to a blank pill) and flag a Plaid-linked account
// as stale when it's gone quiet well past the daily scheduler's cadence.
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { account1 } from '../test/fixtures'
import type { SyncStatus } from '../types/account'
import { SyncStatusPill } from './SyncStatusPill'

const NOW = Date.parse('2026-07-10T00:00:00Z')
const now = () => NOW

function withStatus(status: SyncStatus, lastSyncedAt: string | null = null, lastSyncError: string | null = null) {
  return { ...account1, last_sync_status: status, last_synced_at: lastSyncedAt, last_sync_error: lastSyncError }
}

describe('SyncStatusPill', () => {
  it('renders nothing for a manual (non-Plaid) account', () => {
    const { container } = render(<SyncStatusPill account={account1} now={now} />)
    expect(container).toBeEmptyDOMElement()
  })

  it.each<[SyncStatus, string]>([
    ['never', 'Not synced'],
    ['syncing', 'Syncing…'],
    ['error', 'Sync failed'],
    ['reauth_required', 'Needs reconnect'],
  ])('labels %s as %s', (status, expected) => {
    render(<SyncStatusPill account={withStatus(status)} now={now} />)
    expect(screen.getByText(expected)).toBeInTheDocument()
  })

  it('labels ok_with_errors distinctly from a clean ok sync', () => {
    render(<SyncStatusPill account={withStatus('ok_with_errors', '2026-07-09T23:00:00Z')} now={now} />)
    expect(screen.getByText(/errors/)).toBeInTheDocument()
  })

  it('is not flagged stale just after a fresh ok sync', () => {
    render(<SyncStatusPill account={withStatus('ok', '2026-07-09T23:00:00Z')} now={now} />)
    expect(screen.queryByText('stale')).not.toBeInTheDocument()
  })

  it('flags an ok-status item stale after 3+ days of silence', () => {
    render(<SyncStatusPill account={withStatus('ok', '2026-07-06T00:00:00Z')} now={now} />)
    expect(screen.getByText('stale')).toBeInTheDocument()
  })

  it('flags a stale ok_with_errors item too', () => {
    render(<SyncStatusPill account={withStatus('ok_with_errors', '2026-07-01T00:00:00Z')} now={now} />)
    expect(screen.getByText('stale')).toBeInTheDocument()
  })

  it('does not pile a stale flag onto an already-erroring item', () => {
    render(<SyncStatusPill account={withStatus('error', '2026-07-01T00:00:00Z', 'boom')} now={now} />)
    expect(screen.queryByText('stale')).not.toBeInTheDocument()
  })

  it('expands the error detail for ok_with_errors on click', () => {
    render(
      <SyncStatusPill
        account={withStatus('ok_with_errors', '2026-07-09T23:00:00Z', '2 rows failed to import')}
        now={now}
      />,
    )
    expect(screen.queryByText('2 rows failed to import')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText('2 rows failed to import')).toBeInTheDocument()
  })
})
