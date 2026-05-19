package household

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// PurgeResult is what RunPurge returns to the operator-facing CLI.
// MembersPurged counts household_members rows that received purged_at;
// SharesDeleted counts account_shares rows physically removed.
type PurgeResult struct {
	MembersPurged int
	SharesDeleted int
}

// RunPurge seals every in-grace member whose grace window has elapsed:
// physically deletes their household-scoped account_shares and sets
// `purged_at = now`. Idempotent — re-running on a clean DB is a no-op.
//
// Correctness note: the aggregator (ADR-0008) already excludes in-grace
// members from LIVE aggregates and runs the same lifecycle filter on
// every read, so the system's read-side behavior is identical with or
// without this runner ever running. The runner exists purely to bound
// disk growth and to lift the unique-on-(household_id, user_id) WHERE
// purged_at IS NULL invariant so re-joins after grace can land cleanly.
//
// `now` is injected so the cmd can pin a deterministic clock for the
// integration test; production callers pass `time.Now()`.
func RunPurge(ctx context.Context, db *gorm.DB, now time.Time) (PurgeResult, error) {
	var res PurgeResult
	if db == nil {
		return res, errors.New("household: RunPurge requires a non-nil *gorm.DB")
	}

	// Step 1: select the in-grace rows whose grace has elapsed. The join
	// pulls the per-household grace value so we honor owner-tuned values.
	// `WHERE h.deleted_at IS NULL` skips dissolved households — those
	// rows are a separate cleanup concern.
	type expiredRow struct {
		MemberID    int64
		UserID      int64
		HouseholdID int64
	}
	var expired []expiredRow
	if err := db.WithContext(ctx).Raw(`
		SELECT m.id AS member_id, m.user_id, m.household_id
		FROM household_members m
		JOIN households h ON h.id = m.household_id
		WHERE m.purged_at IS NULL
		  AND m.left_at IS NOT NULL
		  AND h.deleted_at IS NULL
		  AND m.left_at + (h.grace_period_days * INTERVAL '1 day') < ?
		ORDER BY m.id
	`, now).Scan(&expired).Error; err != nil {
		return res, fmt.Errorf("list expired in-grace members: %w", err)
	}
	if len(expired) == 0 {
		return res, nil
	}

	// Step 2: for each expired member, atomically delete their household
	// account_shares and seal the membership row. One tx per member so a
	// mid-flight failure on one member doesn't undo earlier successes —
	// the runner is meant to be re-runnable.
	for _, e := range expired {
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Physically delete account_shares for accounts the purged user
			// owns in this household. Hard-delete (Unscoped) so the row
			// truly leaves the table — soft-deleting would leave it
			// readable by the aggregator's reflection-only privacy checks
			// and is the wrong semantic for "grace expired, link gone".
			sharesRes := tx.Exec(`
				DELETE FROM account_shares
				WHERE household_id = ?
				  AND account_id IN (
				      SELECT id FROM accounts
				      WHERE user_id = ?
				  )
			`, e.HouseholdID, e.UserID)
			if sharesRes.Error != nil {
				return fmt.Errorf("delete shares for user %d in household %d: %w",
					e.UserID, e.HouseholdID, sharesRes.Error)
			}

			// Seal the membership.
			memRes := tx.Model(&model.HouseholdMember{}).
				Where("id = ? AND purged_at IS NULL", e.MemberID).
				Update("purged_at", now)
			if memRes.Error != nil {
				return fmt.Errorf("seal member %d: %w", e.MemberID, memRes.Error)
			}
			if memRes.RowsAffected == 0 {
				// Concurrent purge — someone else already sealed this
				// row. Treat as benign; the shares delete above is also
				// idempotent.
				return nil
			}

			res.MembersPurged++
			res.SharesDeleted += int(sharesRes.RowsAffected)
			return nil
		})
		if err != nil {
			return res, err
		}
	}
	return res, nil
}
