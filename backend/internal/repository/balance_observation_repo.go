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
	// LatestPerAccount returns the single most-recent observation (by as_of,
	// tie-broken by id) for every account_id the user has at least one
	// observation for. Powers the per-account "as of" provenance surfaced on
	// the accounts API (#369) — an account can hold several assets, but the
	// UI only needs one freshness signal per account.
	LatestPerAccount(ctx context.Context, userID int64) ([]model.AccountBalanceObservation, error)
	// ListByAccount returns every observation for the account across all its
	// assets, newest first — the per-account reconciliation view's (#370)
	// observation-history half.
	ListByAccount(ctx context.Context, userID, accountID int64) ([]model.AccountBalanceObservation, error)
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

func (r *balanceObservationRepo) ListByAccount(ctx context.Context, userID, accountID int64) ([]model.AccountBalanceObservation, error) {
	var out []model.AccountBalanceObservation
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND account_id = ?", userID, accountID).
		Order("as_of DESC, id DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *balanceObservationRepo) LatestPerAccount(ctx context.Context, userID int64) ([]model.AccountBalanceObservation, error) {
	var out []model.AccountBalanceObservation
	if err := r.db.WithContext(ctx).
		Raw(`SELECT DISTINCT ON (account_id) *
			FROM account_balance_observations
			WHERE user_id = ?
			ORDER BY account_id, as_of DESC, id DESC`, userID).
		Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
