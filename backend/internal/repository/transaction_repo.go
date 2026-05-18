package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gregwym/offbook/backend/internal/model"
)

// TransactionFilter narrows the set of transactions returned by List.
// Zero values mean "no filter on that dimension"; nil pointers mean the same.
// UncategorizedOnly takes precedence over CategoryID — set at most one.
type TransactionFilter struct {
	AccountID         *int64
	CategoryID        *int64
	UncategorizedOnly bool       // matches rows where category_id IS NULL
	From              *time.Time // inclusive lower bound on transaction_date
	To                *time.Time // inclusive upper bound on transaction_date
	Search            string     // ILIKE %term% across description + merchant_name
	Limit             int
	Offset            int
}

// TransactionRepository is the data-access contract for transactions.
// All read paths are scoped by user_id.
type TransactionRepository interface {
	Create(ctx context.Context, t *model.Transaction) error
	GetByID(ctx context.Context, userID, id int64) (*model.Transaction, error)
	List(ctx context.Context, userID int64, f TransactionFilter) ([]model.Transaction, int64, error)
	Update(ctx context.Context, t *model.Transaction) error
	SoftDelete(ctx context.Context, userID, id int64) error
	// CreateBatch inserts many transactions in one round-trip, doing
	// nothing on conflict with the (plaid_transaction_id) unique index.
	// Returns the number of rows actually inserted — caller compares that
	// to len(txns) to derive "skipped as duplicate" if needed.
	CreateBatch(ctx context.Context, txns []model.Transaction) (int64, error)
	// UpdateByPlaidTransactionID overwrites a row keyed by user_id +
	// plaid_transaction_id. The caller supplies the post-merge row; this
	// repo does not enforce field-level merge semantics — see
	// plaid.MergePlaidUpdate for the policy.
	// Returns ErrNotFound if no matching row exists (e.g. a `modified`
	// for a transaction we never saw because it was added+modified+removed
	// across syncs).
	UpdateByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error
	// SoftDeleteByPlaidTransactionID flips deleted_at for a transaction
	// matched by user_id + plaid_transaction_id. Returns ErrNotFound when
	// no live row exists. Idempotent: re-applying on an already-deleted
	// row returns ErrNotFound (because the soft-delete partial index
	// hides the row from the scope).
	SoftDeleteByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string) error
}

type transactionRepo struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) TransactionRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) Create(ctx context.Context, t *model.Transaction) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *transactionRepo) GetByID(ctx context.Context, userID, id int64) (*model.Transaction, error) {
	var t model.Transaction
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *transactionRepo) List(ctx context.Context, userID int64, f TransactionFilter) ([]model.Transaction, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Transaction{}).Where("user_id = ?", userID)

	if f.AccountID != nil {
		q = q.Where("account_id = ?", *f.AccountID)
	}
	switch {
	case f.UncategorizedOnly:
		q = q.Where("category_id IS NULL")
	case f.CategoryID != nil:
		q = q.Where("category_id = ?", *f.CategoryID)
	}
	if f.From != nil {
		q = q.Where("transaction_date >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("transaction_date <= ?", *f.To)
	}
	if s := f.Search; s != "" {
		pattern := "%" + s + "%"
		q = q.Where("description ILIKE ? OR merchant_name ILIKE ?", pattern, pattern)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var out []model.Transaction
	if err := q.
		Order("transaction_date DESC, id DESC").
		Limit(limit).
		Offset(f.Offset).
		Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *transactionRepo) Update(ctx context.Context, t *model.Transaction) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", t.UserID).
		Save(t)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *transactionRepo) CreateBatch(ctx context.Context, txns []model.Transaction) (int64, error) {
	if len(txns) == 0 {
		return 0, nil
	}
	// ON CONFLICT DO NOTHING on the partial unique index
	// `uq_transactions_plaid` (defined in migration 000001 as
	// `(plaid_transaction_id) WHERE deleted_at IS NULL AND plaid_transaction_id IS NOT NULL`).
	// Postgres requires the conflict target to match the partial predicate
	// exactly, hence TargetWhere — without it we get
	// "no unique or exclusion constraint matching the ON CONFLICT specification".
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "plaid_transaction_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "deleted_at IS NULL AND plaid_transaction_id IS NOT NULL"},
		}},
		DoNothing: true,
	}).Create(&txns)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *transactionRepo) UpdateByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error {
	// Save() needs the row's ID populated to issue an UPDATE rather than
	// an INSERT; callers should be passing the merged row from a prior
	// read so ID is set. Defensive: re-resolve if it isn't.
	if merged.ID == 0 {
		var existing model.Transaction
		if err := r.db.WithContext(ctx).
			Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
			First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		merged.ID = existing.ID
		merged.CreatedAt = existing.CreatedAt
	}
	res := r.db.WithContext(ctx).
		Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
		Save(&merged)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *transactionRepo) SoftDeleteByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
		Delete(&model.Transaction{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *transactionRepo) SoftDelete(ctx context.Context, userID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.Transaction{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
