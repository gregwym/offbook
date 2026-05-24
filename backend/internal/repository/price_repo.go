package repository

import (
	"context"
	"errors"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"gorm.io/gorm"
)

// PriceRepository is the read interface for the prices time series. Prices
// are global (not user-scoped) — they describe market observations, not
// holdings. Phase 2 of ADR-0013 adds the write methods.
type PriceRepository interface {
	// LatestPriceAt returns the most recent price for (assetID quoted in
	// quoteAssetID) at or before asOf. Returns ErrNotFound when no row
	// matches — callers decide whether that's a hard error (stale-price
	// guard) or a soft skip.
	LatestPriceAt(ctx context.Context, assetID, quoteAssetID int64, asOf time.Time) (*model.Price, error)

	// ListHistory returns prices for (assetID, quoteAssetID) ordered by
	// as_of ASC, within [from, to). For Phase 1 this is exercised by tests
	// only; the trend chart in Phase 2 consumes it.
	ListHistory(ctx context.Context, assetID, quoteAssetID int64, from, to time.Time) ([]model.Price, error)
}

type priceRepo struct {
	db *gorm.DB
}

func NewPriceRepository(db *gorm.DB) PriceRepository {
	return &priceRepo{db: db}
}

func (r *priceRepo) LatestPriceAt(ctx context.Context, assetID, quoteAssetID int64, asOf time.Time) (*model.Price, error) {
	var p model.Price
	if err := r.db.WithContext(ctx).
		Where("asset_id = ? AND quote_asset_id = ? AND as_of <= ?", assetID, quoteAssetID, asOf).
		Order("as_of DESC").
		First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *priceRepo) ListHistory(ctx context.Context, assetID, quoteAssetID int64, from, to time.Time) ([]model.Price, error) {
	var rows []model.Price
	if err := r.db.WithContext(ctx).
		Where("asset_id = ? AND quote_asset_id = ? AND as_of >= ? AND as_of < ?", assetID, quoteAssetID, from, to).
		Order("as_of ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
