package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gregwym/offbook/backend/internal/model"
)

type InstanceConfigRepository interface {
	Get(ctx context.Context) (*model.InstanceConfig, error)
	Upsert(ctx context.Context, c *model.InstanceConfig) error
}

type instanceConfigRepo struct{ db *gorm.DB }

func NewInstanceConfigRepository(db *gorm.DB) InstanceConfigRepository {
	return &instanceConfigRepo{db: db}
}

func (r *instanceConfigRepo) Get(ctx context.Context) (*model.InstanceConfig, error) {
	var c model.InstanceConfig
	if err := r.db.WithContext(ctx).First(&c, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *instanceConfigRepo) Upsert(ctx context.Context, c *model.InstanceConfig) error {
	c.ID = 1
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"signup_mode", "updated_at"}),
	}).Create(c).Error
}
