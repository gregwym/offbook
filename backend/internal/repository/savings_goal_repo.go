package repository

import (
	"context"
	"errors"
	"time"

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
	// The same transaction appends a savings_goal_contributions row holding
	// delta and the post-update total, recorded as made by `by` (#682).
	AddContribution(ctx context.Context, owner PlanOwner, id int64, delta decimal.Decimal, by int64) (*model.SavingsGoal, error)
	// ListContributions returns a goal's recorded contributions, newest
	// first. The caller must already have checked the goal belongs to the
	// owner; this re-applies the owner filter so a wrong id returns nothing.
	ListContributions(ctx context.Context, owner PlanOwner, id int64) ([]model.SavingsGoalContribution, error)
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

// goalMetadataColumns are the only columns a metadata Update writes.
// current_amount is deliberately absent: it moves only through
// AddContribution's server-side increment, so a stale snapshot read before a
// concurrent contribution can never overwrite that contribution (#684).
var goalMetadataColumns = []string{"name", "target_amount", "target_date", "account_id", "updated_at"}

func (r *savingsGoalRepo) Update(ctx context.Context, g *model.SavingsGoal) error {
	owner := PlanOwner{UserID: g.UserID, HouseholdID: g.HouseholdID}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Select lists the columns explicitly so a cleared (NULL) target_date
		// or account_id is still written, unlike a plain struct Updates.
		res := owner.Apply(tx).
			Model(&model.SavingsGoal{}).
			Where("id = ? AND deleted_at IS NULL", g.ID).
			Select(goalMetadataColumns).
			Updates(&model.SavingsGoal{
				Name:         g.Name,
				TargetAmount: g.TargetAmount,
				TargetDate:   g.TargetDate,
				AccountID:    g.AccountID,
				UpdatedAt:    time.Now(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		// Reload so the caller sees the live current_amount (including any
		// contribution committed after its snapshot), not the stale one.
		return owner.Apply(tx).First(g, g.ID).Error
	})
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

func (r *savingsGoalRepo) AddContribution(ctx context.Context, owner PlanOwner, id int64, delta decimal.Decimal, by int64) (*model.SavingsGoal, error) {
	// Single UPDATE with arithmetic in SQL — concurrent contributions
	// serialize on the row lock the UPDATE takes, which is held until this
	// transaction commits, so each history row's balance_after is the total
	// that UPDATE produced. updated_at is touched explicitly because gorm's
	// auto-update only fires through Save/Updates with the model, not
	// arbitrary expression updates.
	var g model.SavingsGoal
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := owner.Apply(tx).
			Model(&model.SavingsGoal{}).
			Where("id = ? AND deleted_at IS NULL", id).
			Updates(map[string]interface{}{
				"current_amount": gorm.Expr("current_amount + ?", delta),
				"updated_at":     gorm.Expr("NOW()"),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := owner.Apply(tx).First(&g, id).Error; err != nil {
			return err
		}
		c := model.SavingsGoalContribution{
			GoalID:       g.ID,
			UserID:       &by,
			Amount:       delta,
			BalanceAfter: g.CurrentAmount,
		}
		return tx.Create(&c).Error
	})
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *savingsGoalRepo) ListContributions(ctx context.Context, owner PlanOwner, id int64) ([]model.SavingsGoalContribution, error) {
	goals := owner.Apply(r.db.WithContext(ctx).Model(&model.SavingsGoal{})).
		Select("id").
		Where("id = ? AND deleted_at IS NULL", id)
	out := []model.SavingsGoalContribution{}
	if err := r.db.WithContext(ctx).
		Where("goal_id IN (?)", goals).
		Order("id DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
