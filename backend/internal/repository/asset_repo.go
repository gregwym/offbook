package repository

import (
	"context"
	"errors"

	"github.com/gregwym/offbook/backend/internal/model"
	"gorm.io/gorm"
)

// AssetRepository is the read interface for the assets reference table.
// Assets are global (not user-scoped) — they describe units of value
// (USD, AAPL, BTC), not anyone's holdings. Phase 1 of ADR-0013 exposes
// only the lookups the rest of the system needs; write paths land in
// Phase 2 once services start consuming positions/prices.
type AssetRepository interface {
	GetByID(ctx context.Context, id int64) (*model.Asset, error)
	GetBySymbolKind(ctx context.Context, symbol, kind string) (*model.Asset, error)
	ListByKind(ctx context.Context, kind string) ([]model.Asset, error)
}

type assetRepo struct {
	db *gorm.DB
}

func NewAssetRepository(db *gorm.DB) AssetRepository {
	return &assetRepo{db: db}
}

func (r *assetRepo) GetByID(ctx context.Context, id int64) (*model.Asset, error) {
	var a model.Asset
	if err := r.db.WithContext(ctx).First(&a, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *assetRepo) GetBySymbolKind(ctx context.Context, symbol, kind string) (*model.Asset, error) {
	var a model.Asset
	if err := r.db.WithContext(ctx).
		Where("symbol = ? AND kind = ?", symbol, kind).
		First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *assetRepo) ListByKind(ctx context.Context, kind string) ([]model.Asset, error) {
	var rows []model.Asset
	if err := r.db.WithContext(ctx).
		Where("kind = ?", kind).
		Order("symbol").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
