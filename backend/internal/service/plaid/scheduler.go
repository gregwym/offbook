package plaid

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/gregwym/offbook/backend/internal/repository"
)

// SyncScheduler runs the daily background transaction sync (#363). A
// Tailscale-private host (ADR-0016) can't receive Plaid webhooks, so
// freshness comes from polling: once a day, every active plaid_item across
// every user gets a SyncTransactions pass. See
// docs/ADR/0021-plaid-polling-sync.md for the polling-not-webhooks
// rationale, cadence, and jitter design.
type SyncScheduler struct {
	svc      *Service
	itemRepo repository.PlaidItemRepository
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
// (across every user) that itemRepo reports via ListAllActive.
func NewSyncScheduler(svc *Service, itemRepo repository.PlaidItemRepository) *SyncScheduler {
	return &SyncScheduler{
		svc:      svc,
		itemRepo: itemRepo,
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
type SyncScheduleResult struct {
	Synced  int
	Skipped int
	Failed  int
}

// RunOnce sleeps a random jitter, then drains every active plaid_item across
// every user. Per-item isolation: one item's failure (network blip, revoked
// consent) is logged and counted, never aborting the pass for the rest —
// same rationale as prices.Scheduler.RunOnce. TryStartSync is the
// concurrency guard: an item already mid-sync (manual resync in flight) or
// sitting in 'error' (needs #364's re-auth flow first) is atomically skipped
// rather than raced or retry-stormed.
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
	}
	return res
}
