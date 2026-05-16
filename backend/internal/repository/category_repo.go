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
	GetByID(ctx context.Context, id int64) (*model.Category, error)
	List(ctx context.Context) ([]model.Category, error)
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

func (r *categoryRepo) List(ctx context.Context) ([]model.Category, error) {
	var out []model.Category
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
