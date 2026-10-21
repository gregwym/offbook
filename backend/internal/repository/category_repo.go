package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// CategoryRepository reads the system taxonomy (user_id NULL, migration
// 000002) plus user-owned categories (#573). Every read is scoped: a caller
// sees system rows and its own, never another user's.
type CategoryRepository interface {
	// GetVisible fetches one category the owner may reference: a system row,
	// or one owned by *userID. A nil userID (a household-owned plan) sees
	// system rows only. Another user's category is ErrNotFound.
	GetVisible(ctx context.Context, userID *int64, id int64) (*model.Category, error)
	// List returns the system taxonomy (user_id NULL) plus the caller's
	// own categories.
	List(ctx context.Context, userID int64) ([]model.Category, error)
	// NameTaken reports whether a live category the user can see already
	// has this name (case-insensitive), ignoring exceptID when non-zero.
	NameTaken(ctx context.Context, userID int64, name string, exceptID int64) (bool, error)
	// SlugTaken reports whether the user already owns a live category with
	// this slug (the uq_categories_slug namespace).
	SlugTaken(ctx context.Context, userID int64, slug string) (bool, error)
	Create(ctx context.Context, c *model.Category) error
	// Rename sets the name of a category the user owns. ErrNotFound when
	// the row is missing, a system row, or another user's.
	Rename(ctx context.Context, userID, id int64, name string) error
}

type categoryRepo struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepo{db: db}
}

func (r *categoryRepo) GetVisible(ctx context.Context, userID *int64, id int64) (*model.Category, error) {
	q := r.db.WithContext(ctx)
	if userID == nil {
		q = q.Where("user_id IS NULL")
	} else {
		q = q.Where("user_id IS NULL OR user_id = ?", *userID)
	}
	var c model.Category
	if err := q.First(&c, id).Error; err != nil {
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

func (r *categoryRepo) NameTaken(ctx context.Context, userID int64, name string, exceptID int64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Category{}).
		Where("(user_id IS NULL OR user_id = ?) AND lower(name) = ? AND id <> ?",
			userID, strings.ToLower(name), exceptID).
		Count(&n).Error
	return n > 0, err
}

func (r *categoryRepo) SlugTaken(ctx context.Context, userID int64, slug string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Category{}).
		Where("user_id = ? AND slug = ?", userID, slug).
		Count(&n).Error
	return n > 0, err
}

func (r *categoryRepo) Create(ctx context.Context, c *model.Category) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *categoryRepo) Rename(ctx context.Context, userID, id int64, name string) error {
	res := r.db.WithContext(ctx).Model(&model.Category{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("name", name)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
