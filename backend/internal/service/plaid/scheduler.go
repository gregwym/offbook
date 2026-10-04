package plaid

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// SyncScheduler runs the daily background sync (#363, extended by #369). A
// Tailscale-private host (ADR-0016) can't receive Plaid webhooks, so
// freshness comes from polling: once a day, every active plaid_item across
// every user gets an accounts + transactions pass, plus a holdings pass for
// items with at least one local investment-type account. See
// docs/ADR/0021-plaid-polling-sync.md for the polling-not-webhooks
// rationale, cadence, and jitter design, and its #369 addendum for the
// balance/holdings extension.
type SyncScheduler struct {
	svc      *Service
	itemRepo repository.PlaidItemRepository
	acctRepo repository.AccountRepository
	// jitter randomizes each run's start within this window so a
	// self-hosted instance doesn't call Plaid at the exact same wall-clock
	// moment every day.
	jitter time.Duration
	// pause between items keeps a multi-item instance inside Plaid's
	// per-key rate limits — same rationale as prices.Scheduler.pause.
	pause time.Duration
	logf  func(format string, args ...any)
}

// NewSyncScheduler wires a daily-pass scheduler over every active plaid_item
// (across every user) that itemRepo reports via ListAllActive. acctRepo is
// used only to decide whether an item's holdings are worth syncing (#369) —
// it never crosses user_id.
func NewSyncScheduler(svc *Service, itemRepo repository.PlaidItemRepository, acctRepo repository.AccountRepository) *SyncScheduler {
	return &SyncScheduler{
		svc:      svc,
		itemRepo: itemRepo,
		acctRepo: acctRepo,
		jitter:   30 * time.Minute,
		pause:    5 * time.Second,
		logf:     log.Printf,
	}
}

// WithJitter overrides the pre-run randomized delay (tests).
func (s *SyncScheduler) WithJitter(d time.Duration) *SyncScheduler {
	s.jitter = d
	return s
}

// WithPause overrides the between-items pause (tests).
func (s *SyncScheduler) WithPause(d time.Duration) *SyncScheduler {
	s.pause = d
	return s
}

// SyncScheduleResult summarizes one scheduled pass across every active item.
// Synced/Skipped/Failed track the transactions pass (unchanged since #363) —
// that is the pass that owns the item's last_sync_status lifecycle.
// AccountsFailed/HoldingsFailed are separate, non-fatal-to-the-item counters
// for the #369 balance/holdings extension: an accounts or holdings fetch
// failure is logged and counted but never blocks the transactions pass for
// the same item, since balance/holdings freshness is secondary to
// transaction freshness.
type SyncScheduleResult struct {
	Synced         int
	Skipped        int
	Failed         int
	AccountsFailed int
	HoldingsFailed int
}

// RunOnce sleeps a random jitter, then drains every active plaid_item across
// every user. Per-item isolation: one item's failure (network blip, revoked
// consent) is logged and counted, never aborting the pass for the rest —
// same rationale as prices.Scheduler.RunOnce. TryStartSync is the
// concurrency guard: an item already mid-sync (manual resync in flight), in
// generic 'error', or in 'reauth_required' (needs a Link update-mode session
// — see #364) is atomically skipped rather than raced or retry-stormed.
func (s *SyncScheduler) RunOnce(ctx context.Context) SyncScheduleResult {
	var res SyncScheduleResult
	if s.jitter > 0 {
		select {
		case <-ctx.Done():
			return res
		case <-time.After(time.Duration(rand.Int63n(int64(s.jitter)))):
		}
	}

	items, err := s.itemRepo.ListAllActive(ctx)
	if err != nil {
		s.logf("plaid sync scheduler: list items: %v", err)
		return res
	}

	for i, item := range items {
		if i > 0 && s.pause > 0 {
			select {
			case <-ctx.Done():
				return res
			case <-time.After(s.pause):
			}
		}

		started, err := s.itemRepo.TryStartSync(ctx, item.UserID, item.ID)
		if err != nil {
			s.logf("plaid sync scheduler: item %s (user %d): try-start: %v", item.PlaidItemID, item.UserID, err)
			res.Failed++
			continue
		}
		if !started {
			res.Skipped++
			continue
		}

		// Refresh balances first (#369) so the cash reconciliation inside
		// SyncTransactions below (reconcileItemCashPositions) compares the
		// transaction fold against *today's* reported balance, not
		// whatever positions.quantity happened to hold from the last manual
		// resync. A failure here is logged and counted separately — it
		// never blocks the transactions pass, which owns the item's
		// last_sync_status lifecycle (TryStartSync's CAS already flipped the
		// item to 'syncing'; only SyncTransactions resolves it back to
		// ok/error/reauth_required, success or failure).
		if _, err := s.svc.SyncAccounts(ctx, item.UserID, item.PlaidItemID); err != nil {
			s.logf("plaid sync scheduler: item %s (user %d): sync-accounts: %v", item.PlaidItemID, item.UserID, err)
			res.AccountsFailed++
		}

		result, err := s.svc.SyncTransactions(ctx, item.UserID, item.PlaidItemID)
		if err != nil {
			s.logf("plaid sync scheduler: item %s (user %d): %v", item.PlaidItemID, item.UserID, err)
			res.Failed++
			continue
		}
		res.Synced++
		if result.Inserted > 0 || result.Modified > 0 || result.Removed > 0 || result.Failed > 0 {
			s.logf("plaid sync scheduler: item %s (user %d): %d inserted, %d modified, %d removed, %d failed",
				item.PlaidItemID, item.UserID, result.Inserted, result.Modified, result.Removed, result.Failed)
		}

		// Holdings only for items with a local investment-type account —
		// Plaid's /investments/holdings/get errors for items whose Link
		// session never requested the investments product, so skip rather
		// than retry-storm a doomed call for every checking/savings item,
		// every day.
		if s.hasInvestmentAccount(ctx, item) {
			if _, err := s.svc.SyncHoldings(ctx, item.UserID, item.PlaidItemID); err != nil {
				s.logf("plaid sync scheduler: item %s (user %d): sync-holdings: %v", item.PlaidItemID, item.UserID, err)
				res.HoldingsFailed++
			}
		}
	}
	return res
}

// hasInvestmentAccount reports whether any local, non-deleted account for
// this plaid_item is type "investment" — the gate for whether a daily
// holdings sync (#369) is worth attempting at all.
func (s *SyncScheduler) hasInvestmentAccount(ctx context.Context, item model.PlaidItem) bool {
	if s.acctRepo == nil {
		return false
	}
	accounts, err := s.acctRepo.ListByPlaidItemID(ctx, item.UserID, item.PlaidItemID)
	if err != nil {
		s.logf("plaid sync scheduler: item %s (user %d): list accounts for holdings gate: %v", item.PlaidItemID, item.UserID, err)
		return false
	}
	for _, a := range accounts {
		if a.AccountType == "investment" {
			return true
		}
	}
	return false
}
