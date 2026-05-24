package repository

import (
	"context"
	"errors"

	"github.com/gregwym/offbook/backend/internal/model"
	"gorm.io/gorm"
)

// PositionRepository is the read interface for positions. All reads are
// scoped by user_id — there is no cross-tenant "fetch by id" path. Phase 2
// of ADR-0013 adds the write methods.
type PositionRepository interface {
	GetByID(ctx context.Context, userID, id int64) (*model.Position, error)
	ListByAccountID(ctx context.Context, userID, accountID int64) ([]model.Position, error)
	ListByUserID(ctx context.Context, userID int64) ([]model.Position, error)
}

type positionRepo struct {
	db *gorm.DB
}

func NewPositionRepository(db *gorm.DB) PositionRepository {
	return &positionRepo{db: db}
}

func (r *positionRepo) GetByID(ctx context.Context, userID, id int64) (*model.Position, error) {
	var p model.Position
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *positionRepo) ListByAccountID(ctx context.Context, userID, accountID int64) ([]model.Position, error) {
	var rows []model.Position
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND account_id = ?", userID, accountID).
		Order("id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *positionRepo) ListByUserID(ctx context.Context, userID int64) ([]model.Position, error) {
	var rows []model.Position
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("account_id, id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
