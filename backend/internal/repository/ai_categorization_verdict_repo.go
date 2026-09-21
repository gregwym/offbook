package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gregwym/offbook/backend/internal/model"
)

// AICategorizationVerdictRepository is the data-access contract for the AI
// merchant→category verdict cache (ADR-0022 §6). All reads/writes are
// scoped by user_id.
type AICategorizationVerdictRepository interface {
	// Get returns the cached verdict for (userID, merchantKey), or
	// ErrNotFound on a cache miss.
	Get(ctx context.Context, userID int64, merchantKey string) (*model.AICategorizationVerdict, error)
	// Upsert writes a verdict, overwriting any existing row for the same
	// (user_id, merchant_key) — a merchant's category guess can improve
	// across calls (better model, more context), so the latest verdict wins.
	Upsert(ctx context.Context, v *model.AICategorizationVerdict) error
	// ListByUser returns every cached verdict for userID, newest first —
	// the source list for the frontend's "promote to rule" affordance.
	ListByUser(ctx context.Context, userID int64) ([]model.AICategorizationVerdict, error)
}

type aiCategorizationVerdictRepo struct {
	db *gorm.DB
}

func NewAICategorizationVerdictRepository(db *gorm.DB) AICategorizationVerdictRepository {
	return &aiCategorizationVerdictRepo{db: db}
}

func (r *aiCategorizationVerdictRepo) Get(ctx context.Context, userID int64, merchantKey string) (*model.AICategorizationVerdict, error) {
	var v model.AICategorizationVerdict
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND merchant_key = ?", userID, merchantKey).
		First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &v, nil
}

func (r *aiCategorizationVerdictRepo) Upsert(ctx context.Context, v *model.AICategorizationVerdict) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "merchant_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"category_id", "confidence", "provider", "updated_at"}),
	}).Create(v).Error
}

func (r *aiCategorizationVerdictRepo) ListByUser(ctx context.Context, userID int64) ([]model.AICategorizationVerdict, error) {
	var out []model.AICategorizationVerdict
	if err := r.db.WithContext(ctx).
		Preload("Category").
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
