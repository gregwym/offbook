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
	UpdateStatus(ctx context.Context, userID, id int64, status string, lastError *string) error
	// UpdateCursor persists the /transactions/sync cursor + last sync time.
	// Called after each successful pagination flush so a crash mid-pull
	// resumes from the last committed page.
	UpdateCursor(ctx context.Context, userID, id int64, cursor string, lastSyncedAt time.Time) error
	SoftDelete(ctx context.Context, userID, id int64) error
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

func (r *plaidItemRepo) UpdateStatus(ctx context.Context, userID, id int64, status string, lastError *string) error {
	res := r.db.WithContext(ctx).
		Model(&model.PlaidItem{}).
		Where("user_id = ? AND id = ?", userID, id).
		Updates(map[string]any{
			"status":     status,
			"last_error": lastError,
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
	res := r.db.WithContext(ctx).
		Model(&model.PlaidItem{}).
		Where("user_id = ? AND id = ?", userID, id).
		Updates(map[string]any{
			"cursor":         cursor,
			"last_synced_at": lastSyncedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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
