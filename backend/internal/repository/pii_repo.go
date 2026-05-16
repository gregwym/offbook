// Package repository — pii_repo is the ONLY repository that touches pii_store.
// See ARCHITECTURE.md "PII Isolation". Other services and the AI layer MUST NOT
// depend on this type.
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gregwym/offbook/backend/internal/model"
)

// PIIRepository is the data-access contract for pii_store. The interface is
// deliberately narrow — there is no List, no search, no cross-entity queries.
// PII lookups are always scoped to a specific (entity_type, entity_id).
type PIIRepository interface {
	Set(ctx context.Context, entityType string, entityID int64, field, value string) error
	Get(ctx context.Context, entityType string, entityID int64, field string) (string, error)
	GetAll(ctx context.Context, entityType string, entityID int64) (map[string]string, error)
	DeleteAll(ctx context.Context, entityType string, entityID int64) error
}

type piiRepo struct {
	db *gorm.DB
}

func NewPIIRepository(db *gorm.DB) PIIRepository {
	return &piiRepo{db: db}
}

func (r *piiRepo) Set(ctx context.Context, entityType string, entityID int64, field, value string) error {
	rec := model.PIIRecord{
		EntityType: entityType,
		EntityID:   entityID,
		FieldName:  field,
		Value:      value,
	}
	// Upsert on the (entity_type, entity_id, field_name) unique constraint.
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "field_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&rec).Error
}

func (r *piiRepo) Get(ctx context.Context, entityType string, entityID int64, field string) (string, error) {
	var rec model.PIIRecord
	err := r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ? AND field_name = ?", entityType, entityID, field).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return rec.Value, nil
}

func (r *piiRepo) GetAll(ctx context.Context, entityType string, entityID int64) (map[string]string, error) {
	var recs []model.PIIRecord
	if err := r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Find(&recs).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(recs))
	for _, rec := range recs {
		out[rec.FieldName] = rec.Value
	}
	return out, nil
}

func (r *piiRepo) DeleteAll(ctx context.Context, entityType string, entityID int64) error {
	return r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Delete(&model.PIIRecord{}).Error
}
