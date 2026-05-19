package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// InvestmentRepository is the data-access contract for investment snapshots.
// All reads are scoped by user_id. The table is append-only by design —
// there is no Update or SoftDelete (see docs/ARCHITECTURE.md: snapshot
// tables don't carry deleted_at because silently corrupting historical
// state is worse than keeping a wrong row).
type InvestmentRepository interface {
	Create(ctx context.Context, inv *model.Investment) error
	GetByID(ctx context.Context, userID, id int64) (*model.Investment, error)
	// ListLatestPerHolding returns the most-recent snapshot for each
	// (account_id, ticker) pair belonging to the user. One Postgres query
	// via DISTINCT ON.
	ListLatestPerHolding(ctx context.Context, userID int64) ([]model.Investment, error)
	// ListSnapshotsByHolding returns every snapshot for a single
	// (user_id, account_id, ticker), oldest first. ticker matching is
	// case-insensitive (we always store uppercase, but defend against
	// callers that send lowercase).
	ListSnapshotsByHolding(ctx context.Context, userID, accountID int64, ticker string) ([]model.Investment, error)
}

type investmentRepo struct {
	db *gorm.DB
}

func NewInvestmentRepository(db *gorm.DB) InvestmentRepository {
	return &investmentRepo{db: db}
}

func (r *investmentRepo) Create(ctx context.Context, inv *model.Investment) error {
	return r.db.WithContext(ctx).Create(inv).Error
}

func (r *investmentRepo) GetByID(ctx context.Context, userID, id int64) (*model.Investment, error) {
	var inv model.Investment
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&inv, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &inv, nil
}

func (r *investmentRepo) ListLatestPerHolding(ctx context.Context, userID int64) ([]model.Investment, error) {
	// DISTINCT ON over (account_id, ticker) ordered by snapshot_date DESC,
	// id DESC picks the freshest snapshot per holding. Wrap in an outer
	// SELECT so the final result can be ordered however we like (here:
	// stable by account_id, then ticker) without breaking the DISTINCT ON
	// requirement that its ORDER BY start with the same columns.
	var out []model.Investment
	err := r.db.WithContext(ctx).Raw(`
		SELECT * FROM (
			SELECT DISTINCT ON (account_id, ticker) *
			FROM investments
			WHERE user_id = ?
			ORDER BY account_id, ticker, snapshot_date DESC, id DESC
		) latest
		ORDER BY account_id, ticker
	`, userID).Scan(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *investmentRepo) ListSnapshotsByHolding(ctx context.Context, userID, accountID int64, ticker string) ([]model.Investment, error) {
	tk := strings.ToUpper(strings.TrimSpace(ticker))
	var out []model.Investment
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND account_id = ? AND UPPER(ticker) = ?", userID, accountID, tk).
		Order("snapshot_date ASC, id ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}
