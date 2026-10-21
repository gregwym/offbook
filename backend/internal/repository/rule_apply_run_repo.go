package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gregwym/offbook/backend/internal/model"
)

// RuleApplyRunRepository stores committed rule apply runs and the prior
// state of the rows each changed (#565), so a run can be undone. All reads
// are user-scoped.
type RuleApplyRunRepository interface {
	Create(ctx context.Context, run *model.RuleApplyRun) error
	// AddChanges records changed rows for runID and adds len(changes) to the
	// run's rows_changed.
	AddChanges(ctx context.Context, runID int64, changes []model.RuleApplyChange) error
	// Latest returns the user's most recent run, or ErrNotFound.
	Latest(ctx context.Context, userID int64) (*model.RuleApplyRun, error)
	// GetForUpdate returns the run iff it belongs to userID, locking it for
	// the surrounding transaction; else ErrNotFound.
	GetForUpdate(ctx context.Context, userID, id int64) (*model.RuleApplyRun, error)
	// Restore writes each recorded prior state back to its transaction,
	// skipping rows modified since the run (updated_at no longer equals
	// applied_updated_at) and deleted rows. Returns the rows restored.
	Restore(ctx context.Context, userID, runID int64) (int, error)
	// MarkUndone stamps the run undone with its restored/kept counts.
	MarkUndone(ctx context.Context, run *model.RuleApplyRun, at time.Time, restored, kept int) error
}

type ruleApplyRunRepo struct {
	db *gorm.DB
}

func NewRuleApplyRunRepository(db *gorm.DB) RuleApplyRunRepository {
	return &ruleApplyRunRepo{db: db}
}

func (r *ruleApplyRunRepo) Create(ctx context.Context, run *model.RuleApplyRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *ruleApplyRunRepo) AddChanges(ctx context.Context, runID int64, changes []model.RuleApplyChange) error {
	if len(changes) == 0 {
		return nil
	}
	for i := range changes {
		changes[i].RunID = runID
	}
	if err := r.db.WithContext(ctx).CreateInBatches(changes, 500).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&model.RuleApplyRun{}).
		Where("id = ?", runID).
		UpdateColumn("rows_changed", gorm.Expr("rows_changed + ?", len(changes))).Error
}

func (r *ruleApplyRunRepo) Latest(ctx context.Context, userID int64) (*model.RuleApplyRun, error) {
	var run model.RuleApplyRun
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id DESC").
		First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &run, nil
}

func (r *ruleApplyRunRepo) GetForUpdate(ctx context.Context, userID, id int64) (*model.RuleApplyRun, error) {
	var run model.RuleApplyRun
	if err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		First(&run, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &run, nil
}

func (r *ruleApplyRunRepo) Restore(ctx context.Context, userID, runID int64) (int, error) {
	res := r.db.WithContext(ctx).Exec(`
		UPDATE transactions t SET
			category_id            = c.prev_category_id,
			categorization_method  = c.prev_categorization_method,
			categorization_rule_id = c.prev_categorization_rule_id,
			merchant_name          = c.prev_merchant_name,
			hidden_at              = c.prev_hidden_at,
			reviewed_at            = c.prev_reviewed_at,
			tags                   = c.prev_tags,
			updated_at             = NOW()
		FROM rule_apply_changes c
		WHERE c.run_id = ?
		  AND t.id = c.transaction_id
		  AND t.user_id = ?
		  AND t.deleted_at IS NULL
		  AND t.updated_at = c.applied_updated_at`, runID, userID)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *ruleApplyRunRepo) MarkUndone(ctx context.Context, run *model.RuleApplyRun, at time.Time, restored, kept int) error {
	run.UndoneAt = &at
	run.RowsRestored = &restored
	run.RowsKept = &kept
	return r.db.WithContext(ctx).Model(run).Updates(map[string]any{
		"undone_at":     at,
		"rows_restored": restored,
		"rows_kept":     kept,
	}).Error
}
