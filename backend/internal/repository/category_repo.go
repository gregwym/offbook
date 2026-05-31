package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// CategoryRepository is read-only for now. M4 will add CRUD when rules ship;
// for M2 we only need existence checks during transaction validation and a
// future read for the frontend dropdown (#32).
type CategoryRepository interface {
	// GetByID fetches one category by id. Not user-scoped: today every
	// category is a system row (user_id NULL). When category CRUD lands
	// (#285), this must gain a user_id filter so a caller can't reference
	// another user's private category.
	GetByID(ctx context.Context, id int64) (*model.Category, error)
	// List returns the system taxonomy (user_id NULL) plus the caller's
	// own categories.
	List(ctx context.Context, userID int64) ([]model.Category, error)
}

type categoryRepo struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepo{db: db}
}

func (r *categoryRepo) GetByID(ctx context.Context, id int64) (*model.Category, error) {
	var c model.Category
	if err := r.db.WithContext(ctx).First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *categoryRepo) List(ctx context.Context, userID int64) ([]model.Category, error) {
	var out []model.Category
	if err := r.db.WithContext(ctx).
		Where("user_id IS NULL OR user_id = ?", userID).
		Order("name ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
