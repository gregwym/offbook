package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// SharedBudgetRepository is the data-access contract for shared_budgets.
// Reads/writes are scoped by household_id — the household service is the
// only caller and enforces membership/role at the service layer. The
// aggregator has its own read-only view of this table; this repository
// owns the CRUD side.
type SharedBudgetRepository interface {
	Create(ctx context.Context, b *model.SharedBudget) error
	GetByID(ctx context.Context, householdID, id int64) (*model.SharedBudget, error)
	List(ctx context.Context, householdID int64) ([]model.SharedBudget, error)
	Update(ctx context.Context, b *model.SharedBudget) error
	SoftDelete(ctx context.Context, householdID, id int64) error
}

type sharedBudgetRepo struct {
	db *gorm.DB
}

func NewSharedBudgetRepository(db *gorm.DB) SharedBudgetRepository {
	return &sharedBudgetRepo{db: db}
}

func (r *sharedBudgetRepo) Create(ctx context.Context, b *model.SharedBudget) error {
	return r.db.WithContext(ctx).Create(b).Error
}

func (r *sharedBudgetRepo) GetByID(ctx context.Context, householdID, id int64) (*model.SharedBudget, error) {
	var b model.SharedBudget
	if err := r.db.WithContext(ctx).
		Where("household_id = ?", householdID).
		First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (r *sharedBudgetRepo) List(ctx context.Context, householdID int64) ([]model.SharedBudget, error) {
	var out []model.SharedBudget
	if err := r.db.WithContext(ctx).
		Where("household_id = ?", householdID).
		Order("is_active DESC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sharedBudgetRepo) Update(ctx context.Context, b *model.SharedBudget) error {
	res := r.db.WithContext(ctx).
		Where("household_id = ?", b.HouseholdID).
		Save(b)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sharedBudgetRepo) SoftDelete(ctx context.Context, householdID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("household_id = ?", householdID).
		Delete(&model.SharedBudget{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
