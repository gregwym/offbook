package plaid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// Sync-error management domain errors.
var (
	// ErrSyncErrorNotFound is returned for missing or already-resolved DLQ
	// rows. Handlers map to 404.
	ErrSyncErrorNotFound = errors.New("plaid sync error not found")

	// ErrSyncErrorReplay is returned when the raw_payload can't be replayed
	// (e.g., its account_id no longer maps to a local account). Handlers
	// surface this as 422 — the row stays unresolved so the owner can
	// dismiss or try again after fixing the underlying cause.
	ErrSyncErrorReplay = errors.New("plaid sync error cannot be retried")
)

// PlaidItemSummary embeds a PlaidItem and adds the unresolved DLQ count.
// Used by /plaid/items so the Settings page can render the badge without
// fanning out N+1 follow-up calls.
type PlaidItemSummary struct {
	model.PlaidItem
	UnresolvedSyncErrors int64 `json:"unresolved_sync_errors"`
}

// ListItemsWithSyncErrors returns every linked item for the user along
// with its unresolved DLQ count. Replaces the bare ListItems call when
// the frontend wants the badge.
func (s *Service) ListItemsWithSyncErrors(ctx context.Context, userID int64) ([]PlaidItemSummary, error) {
	if s == nil || s.itemRepo == nil {
		return nil, ErrNotConfigured
	}
	items, err := s.itemRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]PlaidItemSummary, 0, len(items))
	if len(items) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	counts := map[int64]int64{}
	if s.syncErrRepo != nil {
		counts, err = s.syncErrRepo.UnresolvedCountsByItems(ctx, userID, ids)
		if err != nil {
			return nil, fmt.Errorf("plaid: count sync errors: %w", err)
		}
	}
	for _, it := range items {
		out = append(out, PlaidItemSummary{
			PlaidItem:            it,
			UnresolvedSyncErrors: counts[it.ID],
		})
	}
	return out, nil
}

// ListSyncErrors returns DLQ rows for the given item. unresolvedOnly = true
// hides retried/dismissed rows. Item ownership is verified before the
// listing — a 404 here means "not your item" or "no such item".
func (s *Service) ListSyncErrors(ctx context.Context, userID int64, plaidItemID string, unresolvedOnly bool) ([]model.PlaidSyncError, error) {
	if s == nil || s.itemRepo == nil || s.syncErrRepo == nil {
		return nil, ErrNotConfigured
	}
	item, err := s.itemRepo.GetByPlaidItemID(ctx, userID, plaidItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrItemNotFound
		}
		return nil, err
	}
	rows, err := s.syncErrRepo.ListByItem(ctx, userID, item.ID, unresolvedOnly)
	if err != nil {
		return nil, fmt.Errorf("plaid: list sync errors: %w", err)
	}
	if rows == nil {
		rows = []model.PlaidSyncError{}
	}
	return rows, nil
}

// DismissSyncError marks a DLQ row resolved without retrying it. Idempotent
// at the repo layer — a second click returns ErrSyncErrorNotFound rather
// than reaping the prior resolution timestamp.
func (s *Service) DismissSyncError(ctx context.Context, userID, errorID int64) error {
	if s == nil || s.syncErrRepo == nil {
		return ErrNotConfigured
	}
	err := s.syncErrRepo.MarkResolved(ctx, userID, errorID, model.ResolutionDismissed, time.Now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return ErrSyncErrorNotFound
	}
	return err
}

// RetrySyncError replays raw_payload through the same mapping path as a
// live sync. On success the row is marked resolved=retried_ok. On failure
// the row stays unresolved (no second DLQ row — that would multiply forever
// across retries).
//
// Wrapped in one transaction so the txn-insert and resolution flip commit
// together; a mid-flight failure leaves the row exactly as it was.
func (s *Service) RetrySyncError(ctx context.Context, userID, errorID int64) error {
	if s == nil || s.syncErrRepo == nil || s.txRepo == nil || s.acctRepo == nil || s.db == nil {
		return ErrNotConfigured
	}
	row, err := s.syncErrRepo.Get(ctx, userID, errorID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrSyncErrorNotFound
		}
		return err
	}
	if row.ResolvedAt != nil {
		return ErrSyncErrorNotFound
	}

	var pt PlaidTransaction
	if err := json.Unmarshal(row.RawPayload, &pt); err != nil {
		return fmt.Errorf("%w: bad raw_payload: %v", ErrSyncErrorReplay, err)
	}

	userRules, err := s.loadUserRules(ctx, userID)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		acctRepo := repository.NewAccountRepository(tx)
		syncErrRepo := repository.NewPlaidSyncErrorRepository(tx)
		cache := map[string]int64{}

		// Resurrect-aware insert path, mirroring SyncTransactions.
		soft, err := txRepo.FindSoftDeletedByPlaidTransactionIDs(ctx, userID, []string{pt.PlaidTransactionID})
		if err != nil {
			return fmt.Errorf("plaid: retry lookup soft-deleted: %w", err)
		}
		if len(soft) > 0 {
			localID, err := resolveAccountID(ctx, acctRepo, userID, pt.PlaidAccountID, cache)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrSyncErrorReplay, err)
			}
			merged, err := MergePlaidUpdate(soft[0], pt, localID, s.catMapper, userRules)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrSyncErrorReplay, err)
			}
			if err := txRepo.ResurrectByPlaidTransactionID(ctx, userID, pt.PlaidTransactionID, merged); err != nil {
				return fmt.Errorf("%w: %v", ErrSyncErrorReplay, err)
			}
		} else {
			localID, err := resolveAccountID(ctx, acctRepo, userID, pt.PlaidAccountID, cache)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrSyncErrorReplay, err)
			}
			mapped, err := MapPlaidTransaction(pt, userID, localID, s.catMapper, userRules)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrSyncErrorReplay, err)
			}
			if _, err := txRepo.CreateBatch(ctx, []model.Transaction{mapped}); err != nil {
				return fmt.Errorf("%w: %v", ErrSyncErrorReplay, err)
			}
		}

		if err := syncErrRepo.MarkResolved(ctx, userID, errorID, model.ResolutionRetriedOK, time.Now().UTC()); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// Lost a race with another dismiss — the txn still got
				// inserted; bubble up so the user can refresh and see it.
				return ErrSyncErrorNotFound
			}
			return err
		}
		return nil
	})
}

// looksLikeNumericOverflow lets the retry handler distinguish "data still
// bad" from "replay infra broken" so the UI can render a useful message.
// Kept here (not in service.go) because it's only used by retry.
//
//nolint:unused // reserved for richer retry-error classification (see #80 follow-up)
func looksLikeNumericOverflow(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "numeric field overflow") || strings.Contains(msg, "value out of range")
}
