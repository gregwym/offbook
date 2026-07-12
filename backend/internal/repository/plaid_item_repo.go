package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// PlaidItemRepository owns persistence for plaid_items rows. Every read is
// scoped by user_id — there is no "fetch by id" path that bypasses the
// owner check.
type PlaidItemRepository interface {
	Create(ctx context.Context, item *model.PlaidItem) error
	GetByID(ctx context.Context, userID, id int64) (*model.PlaidItem, error)
	GetByPlaidItemID(ctx context.Context, userID int64, plaidItemID string) (*model.PlaidItem, error)
	ListByUser(ctx context.Context, userID int64) ([]model.PlaidItem, error)
	UpdateStatus(ctx context.Context, userID, id int64, status string, lastSyncError *string) error
	// UpdateCursor persists the /transactions/sync cursor + last sync time.
	// Called after each successful pagination flush so a crash mid-pull
	// resumes from the last committed page. Also flips last_sync_status to
	// 'ok' and clears last_sync_error — the cursor only advances on a
	// successful drain.
	UpdateCursor(ctx context.Context, userID, id int64, cursor string, lastSyncedAt time.Time) error
	// UpdateSyncStatus writes the per-sync lifecycle fields. Called at the
	// start of a sync ('syncing') and on failure ('error' with the message).
	// Success is recorded by UpdateCursor inside the same DB transaction as
	// the rest of the sync, so there's no separate UpdateSyncStatus('ok').
	UpdateSyncStatus(ctx context.Context, userID, id int64, status string, syncError *string) error
	SoftDelete(ctx context.Context, userID, id int64) error
	// ListAllActive returns every non-deleted, status='active' plaid_item
	// across all users, ordered by id. It exists for the scheduled background
	// sync job (#363) — the one caller expected to iterate cross-user, the
	// same pattern UserSettingsRepository.ListAutoRefreshUserIDs and
	// household.RunPurge already use for their own periodic jobs. Every other
	// read on this repository stays scoped to a single session's user_id.
	ListAllActive(ctx context.Context) ([]model.PlaidItem, error)
	// TryStartSync atomically flips last_sync_status to 'syncing' unless it
	// is already 'syncing' or 'error' ('error' is skipped until #364's
	// re-auth flow lands — retrying it blind would just retry-storm a
	// broken item). Returns false (no error) when the CAS didn't apply,
	// meaning the caller should skip this item this pass. The scheduled
	// sync job (#363) uses this to avoid racing a concurrent manual resync
	// of the same item; SyncTransactions itself still calls
	// UpdateSyncStatus("syncing", ...) right after, which is a harmless
	// idempotent overwrite.
	TryStartSync(ctx context.Context, userID, id int64) (bool, error)
}

type plaidItemRepo struct {
	db *gorm.DB
}

func NewPlaidItemRepository(db *gorm.DB) PlaidItemRepository {
	return &plaidItemRepo{db: db}
}

func (r *plaidItemRepo) Create(ctx context.Context, item *model.PlaidItem) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *plaidItemRepo) GetByID(ctx context.Context, userID, id int64) (*model.PlaidItem, error) {
	var item model.PlaidItem
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (r *plaidItemRepo) GetByPlaidItemID(ctx context.Context, userID int64, plaidItemID string) (*model.PlaidItem, error) {
	var item model.PlaidItem
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND plaid_item_id = ?", userID, plaidItemID).
		First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (r *plaidItemRepo) ListByUser(ctx context.Context, userID int64) ([]model.PlaidItem, error) {
	var items []model.PlaidItem
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *plaidItemRepo) UpdateStatus(ctx context.Context, userID, id int64, status string, lastSyncError *string) error {
	res := r.db.WithContext(ctx).
		Model(&model.PlaidItem{}).
		Where("user_id = ? AND id = ?", userID, id).
		Updates(map[string]any{
			"status":          status,
			"last_sync_error": lastSyncError,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *plaidItemRepo) UpdateCursor(ctx context.Context, userID, id int64, cursor string, lastSyncedAt time.Time) error {
	// The cursor only advances on a successful drain — so this is also the
	// success terminus for the per-sync lifecycle. Clear any prior error and
	// flip status to 'ok' in the same write so the UI doesn't have to stitch
	// these together across rows.
	res := r.db.WithContext(ctx).
		Model(&model.PlaidItem{}).
		Where("user_id = ? AND id = ?", userID, id).
		Updates(map[string]any{
			"cursor":           cursor,
			"last_synced_at":   lastSyncedAt,
			"last_sync_status": "ok",
			"last_sync_error":  nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *plaidItemRepo) UpdateSyncStatus(ctx context.Context, userID, id int64, status string, syncError *string) error {
	res := r.db.WithContext(ctx).
		Model(&model.PlaidItem{}).
		Where("user_id = ? AND id = ?", userID, id).
		Updates(map[string]any{
			"last_sync_status": status,
			"last_sync_error":  syncError,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *plaidItemRepo) ListAllActive(ctx context.Context) ([]model.PlaidItem, error) {
	var items []model.PlaidItem
	if err := r.db.WithContext(ctx).
		Where("status = ?", "active").
		Order("id").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *plaidItemRepo) TryStartSync(ctx context.Context, userID, id int64) (bool, error) {
	res := r.db.WithContext(ctx).
		Model(&model.PlaidItem{}).
		Where("user_id = ? AND id = ? AND last_sync_status NOT IN (?, ?)", userID, id, "syncing", "error").
		Updates(map[string]any{"last_sync_status": "syncing"})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *plaidItemRepo) SoftDelete(ctx context.Context, userID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.PlaidItem{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
