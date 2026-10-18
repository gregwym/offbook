package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// IngestionJobRepository is the data-access contract for ingestion_jobs — the
// per-import audit trail and AI staging store (ADR-0019 §7). All reads are
// user-scoped: a job is only ever returned to the user that created it.
type IngestionJobRepository interface {
	Create(ctx context.Context, job *model.IngestionJob) error
	// GetForUser returns the job iff it belongs to userID, else ErrNotFound.
	GetForUser(ctx context.Context, userID, id int64) (*model.IngestionJob, error)
	// Update persists a full row (status, rows_imported, completed_at, …).
	Update(ctx context.Context, job *model.IngestionJob) error
	// ListCompletedByAccount returns the user's completed imports into one
	// account, newest first, WITHOUT the staged extraction blob (history is a
	// read-only audit view; undone imports are included). Empty slice for a foreign/unknown account.
	ListCompletedByAccount(ctx context.Context, userID, accountID int64, limit int) ([]model.IngestionJob, error)
	// FindCompletedByHash returns the most recent completed import of a document
	// with this content hash into the account, or ErrNotFound.
	FindCompletedByHash(ctx context.Context, userID, accountID int64, hash string) (*model.IngestionJob, error)
}

type ingestionJobRepo struct {
	db *gorm.DB
}

func NewIngestionJobRepository(db *gorm.DB) IngestionJobRepository {
	return &ingestionJobRepo{db: db}
}

func (r *ingestionJobRepo) Create(ctx context.Context, job *model.IngestionJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *ingestionJobRepo) GetForUser(ctx context.Context, userID, id int64) (*model.IngestionJob, error) {
	var j model.IngestionJob
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&j, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &j, nil
}

func (r *ingestionJobRepo) Update(ctx context.Context, job *model.IngestionJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *ingestionJobRepo) ListCompletedByAccount(ctx context.Context, userID, accountID int64, limit int) ([]model.IngestionJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	jobs := []model.IngestionJob{}
	err := r.db.WithContext(ctx).
		Omit("extraction").
		Where("user_id = ? AND account_id = ? AND status = ?", userID, accountID, "completed").
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (r *ingestionJobRepo) FindCompletedByHash(ctx context.Context, userID, accountID int64, hash string) (*model.IngestionJob, error) {
	var j model.IngestionJob
	err := r.db.WithContext(ctx).
		Omit("extraction").
		Where("user_id = ? AND account_id = ? AND status = ? AND undone_at IS NULL AND content_hash = ?", userID, accountID, "completed", hash).
		Order("created_at DESC, id DESC").
		First(&j).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &j, nil
}
