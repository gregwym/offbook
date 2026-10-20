package repository

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// transferCandidateRow is an in-memory match between two unmatched flow
// transactions, scored by how many days apart they fell. Lower is better;
// ties break toward the lower (i, j) index pair, which — since the source
// slice is ordered by (transaction_date, id) — means the earlier-dated or
// lower-id transaction wins. That keeps ListTransferCandidates deterministic
// across repeated calls on unchanged data.
type transferCandidateRow struct {
	i, j     int
	dateDiff int
}

func (r *transactionRepo) ListTransferCandidates(ctx context.Context, userID int64, windowDays int) ([]TransferCandidate, error) {
	if windowDays < 0 {
		windowDays = 0
	}
	// Pull every unmatched flow transaction for the user once; the pairing
	// itself happens in Go below. Per-user unmatched-flow volume is bounded
	// by how much a person transfers between their own accounts, so this
	// stays cheap without needing a self-join window-function query.
	var txns []model.Transaction
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND kind = ? AND is_transfer = FALSE AND transfer_pair_id IS NULL",
			userID, model.KindFlow).
		Order("transaction_date ASC, id ASC").
		Find(&txns).Error; err != nil {
		return nil, err
	}

	// Pairs the user already said are "not a transfer" (#510) are never
	// proposed again. Mirrors the NOT EXISTS in txn_is_proposed_transfer.
	var dismissals []model.TransferMatchDismissal
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&dismissals).Error; err != nil {
		return nil, err
	}
	dismissed := make(map[[2]int64]bool, len(dismissals))
	for _, d := range dismissals {
		dismissed[[2]int64{d.TransactionLowID, d.TransactionHighID}] = true
	}

	var candidates []transferCandidateRow
	for i := range txns {
		for j := i + 1; j < len(txns); j++ {
			a, b := &txns[i], &txns[j]
			diff := daysBetween(a.TransactionDate, b.TransactionDate)
			if diff > windowDays {
				// txns is date-ordered, so every later j for this i is at
				// least as far away — nothing further in the inner loop can
				// qualify either.
				break
			}
			if a.AccountID == b.AccountID || a.AssetID != b.AssetID {
				continue
			}
			if !a.Amount.Equal(b.Amount.Neg()) {
				continue
			}
			if dismissed[orderedPair(a.ID, b.ID)] {
				continue
			}
			candidates = append(candidates, transferCandidateRow{i: i, j: j, dateDiff: diff})
		}
	}

	// Greedy assignment: best (smallest dateDiff, then earliest/lowest-id
	// pair) match wins first, consuming both its transactions so neither can
	// appear in a second proposed pair.
	sort.SliceStable(candidates, func(x, y int) bool {
		if candidates[x].dateDiff != candidates[y].dateDiff {
			return candidates[x].dateDiff < candidates[y].dateDiff
		}
		if candidates[x].i != candidates[y].i {
			return candidates[x].i < candidates[y].i
		}
		return candidates[x].j < candidates[y].j
	})

	used := make(map[int]bool, len(txns))
	out := make([]TransferCandidate, 0)
	for _, c := range candidates {
		if used[c.i] || used[c.j] {
			continue
		}
		used[c.i] = true
		used[c.j] = true
		out = append(out, TransferCandidate{A: txns[c.i], B: txns[c.j]})
	}
	// Newest first — mirrors the "recent activity" framing the Transactions
	// page already uses for its other review queues.
	sort.SliceStable(out, func(x, y int) bool {
		return out[x].A.TransactionDate.After(out[y].A.TransactionDate)
	})
	return out, nil
}

// daysBetween returns the absolute number of calendar days between two DATE
// columns (always UTC midnight, so the Sub is already day-aligned).
func daysBetween(a, b time.Time) int {
	return int(math.Abs(a.Sub(b).Hours() / 24))
}

func (r *transactionRepo) LinkTransferPair(ctx context.Context, userID, idA, idB int64) (*model.Transaction, *model.Transaction, error) {
	if idA == idB {
		return nil, nil, errors.New("transfer pair requires two distinct transactions")
	}
	var a, b model.Transaction
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).First(&a, idA).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Where("user_id = ?", userID).First(&b, idB).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Model(&model.Transaction{}).Where("id = ? AND user_id = ?", a.ID, userID).
			Updates(map[string]any{"is_transfer": true, "transfer_pair_id": b.ID}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Transaction{}).Where("id = ? AND user_id = ?", b.ID, userID).
			Updates(map[string]any{"is_transfer": true, "transfer_pair_id": a.ID}).Error; err != nil {
			return err
		}
		a.IsTransfer, b.IsTransfer = true, true
		a.TransferPairID, b.TransferPairID = &b.ID, &a.ID
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &a, &b, nil
}

func (r *transactionRepo) UnlinkTransferPair(ctx context.Context, userID, transactionID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t model.Transaction
		if err := tx.Where("user_id = ?", userID).First(&t, transactionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if !t.IsTransfer || t.TransferPairID == nil {
			return ErrNotFound
		}
		partnerID := *t.TransferPairID
		if err := tx.Model(&model.Transaction{}).Where("id = ? AND user_id = ?", t.ID, userID).
			Updates(map[string]any{"is_transfer": false, "transfer_pair_id": nil}).Error; err != nil {
			return err
		}
		// The partner may already be gone (soft-deleted after linking) —
		// that's fine, there's nothing left to unlink on that side.
		if err := tx.Model(&model.Transaction{}).Where("id = ? AND user_id = ?", partnerID, userID).
			Updates(map[string]any{"is_transfer": false, "transfer_pair_id": nil}).Error; err != nil {
			return err
		}
		return nil
	})
}

// orderedPair is the (low, high) key a TransferMatchDismissal is stored under.
func orderedPair(a, b int64) [2]int64 {
	if a > b {
		a, b = b, a
	}
	return [2]int64{a, b}
}

func (r *transactionRepo) DismissTransferPair(ctx context.Context, userID, idA, idB int64) error {
	if idA == idB {
		return errors.New("transfer dismissal requires two distinct transactions")
	}
	pair := orderedPair(idA, idB)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.Transaction{}).
			Where("user_id = ? AND id IN ?", userID, []int64{idA, idB}).
			Count(&n).Error; err != nil {
			return err
		}
		if n != 2 {
			return ErrNotFound
		}
		return tx.Exec(`
			INSERT INTO transfer_match_dismissals (user_id, transaction_low_id, transaction_high_id)
			VALUES (?, ?, ?)
			ON CONFLICT (user_id, transaction_low_id, transaction_high_id) WHERE deleted_at IS NULL DO NOTHING`,
			userID, pair[0], pair[1]).Error
	})
}
