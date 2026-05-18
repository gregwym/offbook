package repository

import (
	"context"

	"gorm.io/gorm"
)

// PlaidCategoryMapping is one row of the plaid_category_map table. We
// don't define a GORM model because the data is read-only at app
// startup — a flat struct keeps the lookup map building trivial.
type PlaidCategoryMapping struct {
	PlaidPrimary  string `gorm:"column:plaid_primary"`
	PlaidDetailed string `gorm:"column:plaid_detailed"`
	CategoryID    int64  `gorm:"column:category_id"`
}

// PlaidCategoryMapRepository loads the Plaid PFC → category mapping.
// The mapping is global (not user-scoped) and is intended to be loaded
// once at service construction and cached in memory.
type PlaidCategoryMapRepository interface {
	LoadAll(ctx context.Context) ([]PlaidCategoryMapping, error)
}

type plaidCategoryMapRepo struct {
	db *gorm.DB
}

func NewPlaidCategoryMapRepository(db *gorm.DB) PlaidCategoryMapRepository {
	return &plaidCategoryMapRepo{db: db}
}

func (r *plaidCategoryMapRepo) LoadAll(ctx context.Context) ([]PlaidCategoryMapping, error) {
	var out []PlaidCategoryMapping
	if err := r.db.WithContext(ctx).
		Table("plaid_category_map").
		Select("plaid_primary, plaid_detailed, category_id").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
