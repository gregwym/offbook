package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// PlaidSyncErrorRepository owns persistence for plaid_sync_errors rows.
// Like every multi-tenant repo, every read/write scopes by user_id.
type PlaidSyncErrorRepository interface {
	Create(ctx context.Context, row *model.PlaidSyncError) error
	Get(ctx context.Context, userID, id int64) (*model.PlaidSyncError, error)
	// ListByItem returns DLQ rows for one Plaid item. If unresolvedOnly is
	// true, rows with resolved_at IS NOT NULL are excluded.
	ListByItem(ctx context.Context, userID, plaidItemID int64, unresolvedOnly bool) ([]model.PlaidSyncError, error)
	// CountUnresolvedByItem returns the badge number — how many DLQ rows
	// the owner still needs to act on. Indexed via
	// ix_plaid_sync_errors_item_unresolved.
	CountUnresolvedByItem(ctx context.Context, userID, plaidItemID int64) (int64, error)
	// UnresolvedCountsByItems returns badge counts for several items in one
	// roundtrip — used by the Linked Institutions list endpoint so it
	// doesn't fan out N+1.
	UnresolvedCountsByItems(ctx context.Context, userID int64, plaidItemIDs []int64) (map[int64]int64, error)
	// MarkResolved sets resolved_at + resolution. ErrNotFound if the row
	// is missing OR already resolved (idempotent guard against
	// double-click).
	MarkResolved(ctx context.Context, userID, id int64, resolution string, at time.Time) error
}

type plaidSyncErrorRepo struct {
	db *gorm.DB
}

func NewPlaidSyncErrorRepository(db *gorm.DB) PlaidSyncErrorRepository {
	return &plaidSyncErrorRepo{db: db}
}

func (r *plaidSyncErrorRepo) Create(ctx context.Context, row *model.PlaidSyncError) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *plaidSyncErrorRepo) Get(ctx context.Context, userID, id int64) (*model.PlaidSyncError, error) {
	var row model.PlaidSyncError
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (r *plaidSyncErrorRepo) ListByItem(ctx context.Context, userID, plaidItemID int64, unresolvedOnly bool) ([]model.PlaidSyncError, error) {
	q := r.db.WithContext(ctx).
		Where("user_id = ? AND plaid_item_id = ?", userID, plaidItemID)
	if unresolvedOnly {
		q = q.Where("resolved_at IS NULL")
	}
	var rows []model.PlaidSyncError
	if err := q.Order("occurred_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *plaidSyncErrorRepo) CountUnresolvedByItem(ctx context.Context, userID, plaidItemID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).
		Model(&model.PlaidSyncError{}).
		Where("user_id = ? AND plaid_item_id = ? AND resolved_at IS NULL", userID, plaidItemID).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *plaidSyncErrorRepo) UnresolvedCountsByItems(ctx context.Context, userID int64, plaidItemIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(plaidItemIDs) == 0 {
		return out, nil
	}
	type row struct {
		PlaidItemID int64
		N           int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&model.PlaidSyncError{}).
		Select("plaid_item_id, COUNT(*) AS n").
		Where("user_id = ? AND plaid_item_id IN ? AND resolved_at IS NULL", userID, plaidItemIDs).
		Group("plaid_item_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.PlaidItemID] = r.N
	}
	return out, nil
}

func (r *plaidSyncErrorRepo) MarkResolved(ctx context.Context, userID, id int64, resolution string, at time.Time) error {
	// Filter on resolved_at IS NULL so a second Retry/Dismiss click is a
	// no-op (ErrNotFound) rather than overwriting the prior resolution
	// timestamp.
	res := r.db.WithContext(ctx).
		Model(&model.PlaidSyncError{}).
		Where("user_id = ? AND id = ? AND resolved_at IS NULL", userID, id).
		Updates(map[string]any{
			"resolved_at": at,
			"resolution":  resolution,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
