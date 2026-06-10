package prices

import (
	"context"
	"log"
	"time"
)

// Scheduler runs the background price refresh (#338 Phase 3, ADR-0014 §3).
// Unlike the manual endpoint — where the click is the consent — background
// egress requires a stored opt-in, so each pass refreshes only users whose
// auto_price_refresh setting is true. Everything else (held-assets-only,
// symbol-list-only egress, idempotent upserts) is the same RefreshForUser
// path the button uses.
type Scheduler struct {
	svc      *Service
	optedIn  func(ctx context.Context) ([]int64, error)
	interval time.Duration
	// pause between users keeps a multi-tenant instance inside provider
	// rate limits (CoinGecko free tier is ~10–30 req/min; one pass costs
	// one CoinGecko call + one Frankfurter call per held currency).
	pause time.Duration
	logf  func(format string, args ...any)
}

// NewScheduler wires a daily-pass scheduler. optedIn supplies the user ids
// to refresh (typically UserSettingsRepository.ListAutoRefreshUserIDs).
func NewScheduler(svc *Service, optedIn func(ctx context.Context) ([]int64, error)) *Scheduler {
	return &Scheduler{
		svc:      svc,
		optedIn:  optedIn,
		interval: 24 * time.Hour,
		pause:    5 * time.Second,
		logf:     log.Printf,
	}
}

// WithInterval overrides the pass interval (tests).
func (s *Scheduler) WithInterval(d time.Duration) *Scheduler {
	s.interval = d
	return s
}

// WithPause overrides the between-users pause (tests).
func (s *Scheduler) WithPause(d time.Duration) *Scheduler {
	s.pause = d
	return s
}

// Start launches the refresh loop in a goroutine and returns immediately.
// One pass runs shortly after boot (prices are likely stale after downtime),
// then every interval. The loop exits when ctx is canceled.
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		// Initial pass after a short grace so boot (migrations, first
		// requests) isn't competing with upstream calls.
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		s.RunOnce(ctx)

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RunOnce(ctx)
			}
		}
	}()
}

// RunOnce executes a single refresh pass over every opted-in user. Errors
// are logged per user and never abort the pass — one user's provider
// failure (or revoked upstream) must not starve the rest. Valuations fall
// back to their stale/partial flags either way.
func (s *Scheduler) RunOnce(ctx context.Context) {
	userIDs, err := s.optedIn(ctx)
	if err != nil {
		s.logf("price scheduler: list opted-in users: %v", err)
		return
	}
	for i, userID := range userIDs {
		if i > 0 && s.pause > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.pause):
			}
		}
		result, err := s.svc.RefreshForUser(ctx, userID)
		if err != nil {
			s.logf("price scheduler: refresh user %d: %v", userID, err)
			continue
		}
		if result.Refreshed > 0 || len(result.Skipped) > 0 {
			s.logf("price scheduler: user %d: %d refreshed, %d skipped", userID, result.Refreshed, len(result.Skipped))
		}
	}
}
