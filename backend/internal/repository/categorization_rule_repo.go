package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// CategorizationRuleRepository is the data-access contract for
// categorization_rules. Every read/write path is scoped by user_id —
// rules are user-private and never shared across tenants.
type CategorizationRuleRepository interface {
	Create(ctx context.Context, r *model.CategorizationRule) error
	GetByID(ctx context.Context, userID, id int64) (*model.CategorizationRule, error)
	List(ctx context.Context, userID int64) ([]model.CategorizationRule, error)
	Update(ctx context.Context, r *model.CategorizationRule) error
	SoftDelete(ctx context.Context, userID, id int64) error
}

type categorizationRuleRepo struct {
	db *gorm.DB
}

func NewCategorizationRuleRepository(db *gorm.DB) CategorizationRuleRepository {
	return &categorizationRuleRepo{db: db}
}

func (r *categorizationRuleRepo) Create(ctx context.Context, rule *model.CategorizationRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *categorizationRuleRepo) GetByID(ctx context.Context, userID, id int64) (*model.CategorizationRule, error) {
	var rule model.CategorizationRule
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &rule, nil
}

func (r *categorizationRuleRepo) List(ctx context.Context, userID int64) ([]model.CategorizationRule, error) {
	var out []model.CategorizationRule
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("priority DESC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *categorizationRuleRepo) Update(ctx context.Context, rule *model.CategorizationRule) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", rule.UserID).
		Save(rule)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *categorizationRuleRepo) SoftDelete(ctx context.Context, userID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.CategorizationRule{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
