package repository

import (
	"context"
	"time"

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
	// DeleteByImportJob removes the observations a statement import recorded.
	// This is the one deliberate exception to append-only: undoing the import
	// reverses the ledger rows, so the balance claim they anchored no longer
	// holds (#483).
	DeleteByImportJob(ctx context.Context, userID, jobID int64) error
	// EarliestAfter returns the as_of of the earliest observation for
	// (account, asset) on a calendar day (UTC) after `after`'s, or nil when
	// none exists. A backdated balance must not move the balance such a
	// later observation already pinned (#669).
	EarliestAfter(ctx context.Context, userID, accountID, assetID int64, after time.Time) (*time.Time, error)
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

func (r *balanceObservationRepo) DeleteByImportJob(ctx context.Context, userID, jobID int64) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND import_job_id = ?", userID, jobID).
		Delete(&model.AccountBalanceObservation{}).Error
}

func (r *balanceObservationRepo) EarliestAfter(ctx context.Context, userID, accountID, assetID int64, after time.Time) (*time.Time, error) {
	var out []time.Time
	if err := r.db.WithContext(ctx).
		Model(&model.AccountBalanceObservation{}).
		Where("user_id = ? AND account_id = ? AND asset_id = ? AND (as_of AT TIME ZONE 'UTC')::date > ?::date",
			userID, accountID, assetID, after.UTC().Format("2006-01-02")).
		Order("as_of ASC").
		Limit(1).
		Pluck("as_of", &out).Error; err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}
