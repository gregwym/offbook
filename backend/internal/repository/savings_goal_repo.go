package repository

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// SavingsGoalRepository is the data-access contract for savings goals. CRUD is
// scoped by PlanOwner — a personal book (user_id) or a household
// (household_id), per ADR-0018.
type SavingsGoalRepository interface {
	Create(ctx context.Context, g *model.SavingsGoal) error
	GetByID(ctx context.Context, owner PlanOwner, id int64) (*model.SavingsGoal, error)
	List(ctx context.Context, owner PlanOwner) ([]model.SavingsGoal, error)
	Update(ctx context.Context, g *model.SavingsGoal) error
	SoftDelete(ctx context.Context, owner PlanOwner, id int64) error
	// AddContribution applies an atomic UPDATE adding `delta` to
	// current_amount and bumps updated_at. Returns the post-update row.
	// A naive read+write+save races under concurrent contributions; this
	// keeps the increment server-side. delta may be negative (withdrawal).
	AddContribution(ctx context.Context, owner PlanOwner, id int64, delta decimal.Decimal) (*model.SavingsGoal, error)
}

type savingsGoalRepo struct {
	db *gorm.DB
}

func NewSavingsGoalRepository(db *gorm.DB) SavingsGoalRepository {
	return &savingsGoalRepo{db: db}
}

func (r *savingsGoalRepo) Create(ctx context.Context, g *model.SavingsGoal) error {
	return r.db.WithContext(ctx).Create(g).Error
}

func (r *savingsGoalRepo) GetByID(ctx context.Context, owner PlanOwner, id int64) (*model.SavingsGoal, error) {
	var g model.SavingsGoal
	if err := owner.Apply(r.db.WithContext(ctx)).
		First(&g, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (r *savingsGoalRepo) List(ctx context.Context, owner PlanOwner) ([]model.SavingsGoal, error) {
	var out []model.SavingsGoal
	if err := owner.Apply(r.db.WithContext(ctx)).
		Order("id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *savingsGoalRepo) Update(ctx context.Context, g *model.SavingsGoal) error {
	res := PlanOwner{UserID: g.UserID, HouseholdID: g.HouseholdID}.
		Apply(r.db.WithContext(ctx)).
		Save(g)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *savingsGoalRepo) SoftDelete(ctx context.Context, owner PlanOwner, id int64) error {
	res := owner.Apply(r.db.WithContext(ctx)).
		Delete(&model.SavingsGoal{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *savingsGoalRepo) AddContribution(ctx context.Context, owner PlanOwner, id int64, delta decimal.Decimal) (*model.SavingsGoal, error) {
	// Single UPDATE with arithmetic in SQL — concurrent contributions
	// serialize on the row's update conflict resolution. updated_at is
	// touched explicitly because gorm's auto-update only fires through
	// Save/Updates with the model, not arbitrary Raw or expression updates.
	res := owner.Apply(r.db.WithContext(ctx)).
		Model(&model.SavingsGoal{}).
		Where("id = ? AND deleted_at IS NULL", id).
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
	return r.GetByID(ctx, owner, id)
}
