package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

type UserRepository interface {
	Create(ctx context.Context, u *model.User) error
	GetByID(ctx context.Context, id int64) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	UpdateLastScope(ctx context.Context, id int64, scope string) error
	Count(ctx context.Context) (int64, error)
}

type userRepo struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) Create(ctx context.Context, u *model.User) error {
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *userRepo) GetByID(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	email = strings.ToLower(strings.TrimSpace(email))
	if err := r.db.WithContext(ctx).Where("LOWER(email) = ?", email).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) UpdateLastScope(ctx context.Context, id int64, scope string) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Update("last_scope", scope)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *userRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
