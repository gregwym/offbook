package repository

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// SharedGoalRepository is the data-access contract for shared_goals.
// Reads/writes are scoped by household_id; the household service is the
// only caller and enforces membership/role.
type SharedGoalRepository interface {
	Create(ctx context.Context, g *model.SharedGoal) error
	GetByID(ctx context.Context, householdID, id int64) (*model.SharedGoal, error)
	List(ctx context.Context, householdID int64) ([]model.SharedGoal, error)
	Update(ctx context.Context, g *model.SharedGoal) error
	SoftDelete(ctx context.Context, householdID, id int64) error
	// AddContribution atomically adds delta to current_amount. Returns
	// the post-update row; ErrNotFound when the goal is gone (soft-
	// deleted or wrong household).
	AddContribution(ctx context.Context, householdID, id int64, delta decimal.Decimal) (*model.SharedGoal, error)
}

type sharedGoalRepo struct {
	db *gorm.DB
}

func NewSharedGoalRepository(db *gorm.DB) SharedGoalRepository {
	return &sharedGoalRepo{db: db}
}

func (r *sharedGoalRepo) Create(ctx context.Context, g *model.SharedGoal) error {
	return r.db.WithContext(ctx).Create(g).Error
}

func (r *sharedGoalRepo) GetByID(ctx context.Context, householdID, id int64) (*model.SharedGoal, error) {
	var g model.SharedGoal
	if err := r.db.WithContext(ctx).
		Where("household_id = ?", householdID).
		First(&g, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (r *sharedGoalRepo) List(ctx context.Context, householdID int64) ([]model.SharedGoal, error) {
	var out []model.SharedGoal
	if err := r.db.WithContext(ctx).
		Where("household_id = ?", householdID).
		Order("id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sharedGoalRepo) Update(ctx context.Context, g *model.SharedGoal) error {
	res := r.db.WithContext(ctx).
		Where("household_id = ?", g.HouseholdID).
		Save(g)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sharedGoalRepo) SoftDelete(ctx context.Context, householdID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("household_id = ?", householdID).
		Delete(&model.SharedGoal{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sharedGoalRepo) AddContribution(ctx context.Context, householdID, id int64, delta decimal.Decimal) (*model.SharedGoal, error) {
	// Mirror personal savings_goal AddContribution — single UPDATE so
	// concurrent contributions serialize on the row.
	res := r.db.WithContext(ctx).
		Model(&model.SharedGoal{}).
		Where("household_id = ? AND id = ? AND deleted_at IS NULL", householdID, id).
		Updates(map[string]interface{}{
			"current_amount": gorm.Expr("current_amount + ?", delta),
			"updated_at":     gorm.Expr("NOW()"),
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return r.GetByID(ctx, householdID, id)
}
