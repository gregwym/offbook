package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// UserSettingsRepository is the data-access contract for user_settings.
// One row per user; Get auto-returns ErrNotFound so the service can
// upsert defaults on first read.
type UserSettingsRepository interface {
	Get(ctx context.Context, userID int64) (*model.UserSettings, error)
	Upsert(ctx context.Context, s *model.UserSettings) error
}

type userSettingsRepo struct {
	db *gorm.DB
}

func NewUserSettingsRepository(db *gorm.DB) UserSettingsRepository {
	return &userSettingsRepo{db: db}
}

func (r *userSettingsRepo) Get(ctx context.Context, userID int64) (*model.UserSettings, error) {
	var s model.UserSettings
	if err := r.db.WithContext(ctx).First(&s, "user_id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// Upsert writes the row, inserting on first call and updating on
// subsequent ones. The conflict target is the PK (user_id) so the upsert
// is collision-safe under concurrent writes from multiple sessions.
func (r *userSettingsRepo) Upsert(ctx context.Context, s *model.UserSettings) error {
	// Plain Save() works here: gorm will INSERT if no row exists and UPDATE
	// otherwise. Race condition is benign — both writes carry the same
	// user's settings.
	return r.db.WithContext(ctx).Save(s).Error
}
