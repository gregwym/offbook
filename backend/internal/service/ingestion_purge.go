package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// DefaultAIStagingRetention is how long an uncommitted AI-import stage lives
// before the purge reclaims it (#337). Seven days: long enough that a user who
// previewed an import can come back the following week and commit, short enough
// that abandoned `extraction` payloads don't accumulate indefinitely.
const DefaultAIStagingRetention = 7 * 24 * time.Hour

// staleStagingWhere selects abandoned AI-import staging rows: still awaiting the
// user's commit (status='extracted'), still carrying a staged payload
// (extraction IS NOT NULL), and older than the retention cutoff. The single `?`
// binds the cutoff timestamp. Kept as one const so the dry-run count and the
// apply update can never drift apart.
const staleStagingWhere = "status = 'extracted' AND extraction IS NOT NULL AND created_at < ?"

// StagingPurgeResult reports how many stale staging rows the purge reclaimed.
type StagingPurgeResult struct {
	JobsPurged int
}

// CountStaleAIStaging returns how many rows PurgeStaleAIStaging would reclaim at
// `now` for the given retention — the dry-run counterpart to the purge.
func CountStaleAIStaging(ctx context.Context, db *gorm.DB, now time.Time, retention time.Duration) (int64, error) {
	if db == nil {
		return 0, errors.New("service: CountStaleAIStaging requires a non-nil *gorm.DB")
	}
	var n int64
	if err := db.WithContext(ctx).
		Table("ingestion_jobs").
		Where(staleStagingWhere, now.Add(-retention)).
		Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count stale AI-staging jobs: %w", err)
	}
	return n, nil
}

// PurgeStaleAIStaging reclaims abandoned AI-import staging rows: for every
// ingestion_jobs row that has sat at status='extracted' with a staged
// `extraction` payload past the retention window, it nulls the payload (freeing
// the potentially large JSONB) and moves the row to a terminal 'failed' state
// with an explanatory error_message.
//
// It deliberately does NOT delete the row: ingestion_jobs is an append-only
// audit trail (see docs/ARCHITECTURE.md), so the import attempt stays on record
// — only the heavy, no-longer-committable staged payload is discarded. Flipping
// out of 'extracted' also removes the ghost job from the "awaiting commit" UI so
// it can't be committed against an emptied stage.
//
// Idempotent: a second run reclaims nothing (the WHERE no longer matches a row
// whose extraction is already NULL / status already 'failed'). `now` is injected
// so callers (and the regression test) can pin a deterministic clock.
func PurgeStaleAIStaging(ctx context.Context, db *gorm.DB, now time.Time, retention time.Duration) (StagingPurgeResult, error) {
	var res StagingPurgeResult
	if db == nil {
		return res, errors.New("service: PurgeStaleAIStaging requires a non-nil *gorm.DB")
	}

	cutoff := now.Add(-retention)
	// Single set-based UPDATE — raw Exec bypasses GORM hooks, so updated_at is
	// set explicitly alongside completed_at.
	r := db.WithContext(ctx).Exec(`
		UPDATE ingestion_jobs
		SET extraction    = NULL,
		    status        = 'failed',
		    error_message = 'expired: AI-staged import was not committed within the retention window; staged payload discarded',
		    completed_at  = ?,
		    updated_at    = ?
		WHERE `+staleStagingWhere,
		now, now, cutoff)
	if r.Error != nil {
		return res, fmt.Errorf("purge stale AI-staging jobs: %w", r.Error)
	}
	res.JobsPurged = int(r.RowsAffected)
	return res, nil
}
