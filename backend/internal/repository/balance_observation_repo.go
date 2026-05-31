package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// BalanceObservationRepository is the append-only writer/reader for the
// account_balance_observations audit log (ADR-0017). Observations are never
// mutated or deleted; they record what a sync source reported over time.
type BalanceObservationRepository interface {
	Insert(ctx context.Context, o *model.AccountBalanceObservation) error
	// ListByAccountAsset returns observations for (account, asset), newest
	// first. Used for audit/debugging the reconciliation trail.
	ListByAccountAsset(ctx context.Context, userID, accountID, assetID int64) ([]model.AccountBalanceObservation, error)
}

type balanceObservationRepo struct {
	db *gorm.DB
}

func NewBalanceObservationRepository(db *gorm.DB) BalanceObservationRepository {
	return &balanceObservationRepo{db: db}
}

func (r *balanceObservationRepo) Insert(ctx context.Context, o *model.AccountBalanceObservation) error {
	return r.db.WithContext(ctx).Create(o).Error
}

func (r *balanceObservationRepo) ListByAccountAsset(ctx context.Context, userID, accountID, assetID int64) ([]model.AccountBalanceObservation, error) {
	var out []model.AccountBalanceObservation
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND account_id = ? AND asset_id = ?", userID, accountID, assetID).
		Order("as_of DESC, id DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
