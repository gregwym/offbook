package repository

import (
	"context"
	"errors"

	"github.com/gregwym/offbook/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PositionRepository is the data-access contract for positions. All reads
// are scoped by user_id — there is no cross-tenant "fetch by id" path.
// A (account_id, asset_id) pair has at most one live row (partial unique
// index uq_positions_account_asset).
type PositionRepository interface {
	GetByID(ctx context.Context, userID, id int64) (*model.Position, error)
	ListByAccountID(ctx context.Context, userID, accountID int64) ([]model.Position, error)
	ListByUserID(ctx context.Context, userID int64) ([]model.Position, error)
	// Upsert sets the position for (account_id, asset_id) to the provided
	// quantity (replacement, not delta — caller owns delta math). When the
	// row already exists, cost_basis is updated only if non-nil on `p`.
	Upsert(ctx context.Context, p *model.Position) error
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

func (r *positionRepo) Upsert(ctx context.Context, p *model.Position) error {
	// Conflict target is the partial unique index uq_positions_account_asset.
	// Postgres requires the index_predicate to match for partial-index UPSERT.
	updates := []string{"quantity", "updated_at"}
	if p.CostBasis != nil {
		updates = append(updates, "cost_basis")
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:     []clause.Column{{Name: "account_id"}, {Name: "asset_id"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoUpdates:   clause.AssignmentColumns(updates),
		}).
		Create(p).Error
}
