package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

type SessionRepository interface {
	Create(ctx context.Context, s *model.Session) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*model.Session, error)
	Touch(ctx context.Context, id int64, lastUsed time.Time) error
	DeleteByTokenHash(ctx context.Context, tokenHash string) error
	DeleteByUserID(ctx context.Context, userID int64) error
}

type sessionRepo struct{ db *gorm.DB }

func NewSessionRepository(db *gorm.DB) SessionRepository {
	return &sessionRepo{db: db}
}

func (r *sessionRepo) Create(ctx context.Context, s *model.Session) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *sessionRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Session, error) {
	var s model.Session
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *sessionRepo) Touch(ctx context.Context, id int64, lastUsed time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", id).
		Update("last_used_at", lastUsed).Error
}

func (r *sessionRepo) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).
		Where("token_hash = ?", tokenHash).
		Delete(&model.Session{}).Error
}

func (r *sessionRepo) DeleteByUserID(ctx context.Context, userID int64) error {
	return r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.Session{}).Error
}
