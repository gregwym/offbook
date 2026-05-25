package repository

import (
	"context"
	"errors"

	"github.com/gregwym/offbook/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AssetRepository is the data-access contract for the assets reference
// table. Assets are global (not user-scoped) — they describe units of
// value (USD, AAPL, BTC), not anyone's holdings.
type AssetRepository interface {
	GetByID(ctx context.Context, id int64) (*model.Asset, error)
	GetBySymbolKind(ctx context.Context, symbol, kind string) (*model.Asset, error)
	ListByKind(ctx context.Context, kind string) ([]model.Asset, error)
	// EnsureBySymbolKind returns the asset for (symbol, kind), creating it
	// with the supplied displayName if absent. Used by Plaid sync and
	// manual entry when a previously-unseen ticker shows up.
	EnsureBySymbolKind(ctx context.Context, symbol, kind, displayName string) (*model.Asset, error)
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

func (r *assetRepo) EnsureBySymbolKind(ctx context.Context, symbol, kind, displayName string) (*model.Asset, error) {
	a := &model.Asset{Symbol: symbol, Kind: kind, DisplayName: &displayName, Precision: 8}
	// ON CONFLICT (symbol, kind) DO NOTHING; if a row already exists, the
	// follow-up SELECT below returns the existing record.
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "symbol"}, {Name: "kind"}},
			DoNothing: true,
		}).
		Create(a).Error; err != nil {
		return nil, err
	}
	// Re-fetch so we pick up the existing row when there was a conflict
	// (Create above returns id=0 in that case on Postgres with ON CONFLICT
	// DO NOTHING).
	return r.GetBySymbolKind(ctx, symbol, kind)
}
