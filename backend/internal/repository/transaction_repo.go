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
	// FindSoftDeletedByPlaidTransactionIDs returns rows whose deleted_at IS
	// NOT NULL and whose (user_id, plaid_transaction_id) matches one of the
	// supplied IDs. Used by the Plaid sync path to detect a re-surfaced
	// transaction that the user (or a prior sync) had previously soft-deleted
	// — instead of inserting a duplicate, the service resurrects the row.
	// Empty input → nil, nil.
	FindSoftDeletedByPlaidTransactionIDs(ctx context.Context, userID int64, plaidTxnIDs []string) ([]model.Transaction, error)
	// ResurrectByPlaidTransactionID clears deleted_at on a soft-deleted row
	// and overlays the merged fields. Returns ErrNotFound when no row exists
	// (live or soft-deleted) for the key. The caller is expected to pass a
	// row produced by plaid.MergePlaidUpdate so user-edited fields survive.
	ResurrectByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error
	// ListForCategorizationScope returns a chunk of non-deleted transactions
	// for the user, ordered by id ASC for cursor-style pagination. Callers
	// drive a loop by passing the last returned row's id as afterID on the
	// next call. The empty slice signals end-of-scan.
	//
	// scope filters the underlying query:
	//   - "all"           — every non-deleted transaction
	//   - "uncategorized" — category_id IS NULL
	//   - "plaid_default" — categorization_method = 'plaid_default'
	//
	// Manual rows (categorization_method = 'manual') are NOT filtered out
	// here — the bulk-apply service inspects each row so it can count
	// "skipped_manual" for the response. For scope='plaid_default' the
	// method filter inherently excludes manual rows.
	ListForCategorizationScope(ctx context.Context, userID int64, scope string, afterID int64, limit int) ([]model.Transaction, error)
}

// CategorizationScopes enumerates the valid `scope` values for
// ListForCategorizationScope and the bulk re-categorize endpoint.
const (
	CategorizationScopeAll           = "all"
	CategorizationScopeUncategorized = "uncategorized"
	CategorizationScopePlaidDefault  = "plaid_default"
)

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
	// `uq_transactions_user_plaid` (migration 000004 — user-scoped per #63):
	//   (user_id, plaid_transaction_id)
	//   WHERE deleted_at IS NULL AND plaid_transaction_id IS NOT NULL
	// Postgres requires the conflict target's columns AND its partial
	// predicate to match the index exactly, hence TargetWhere — without
	// it we get "no unique or exclusion constraint matching the ON CONFLICT
	// specification".
	//
	// Note: this only catches LIVE duplicates. A soft-deleted row with the
	// same (user_id, plaid_transaction_id) sits outside the partial index,
	// so a naive re-insert WOULD create a second row. The Plaid service's
	// resurrect-on-resurface pass (see plaid.Service.SyncTransactions)
	// handles that case by undeleting the existing row instead of inserting.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "plaid_transaction_id"}},
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

func (r *transactionRepo) FindSoftDeletedByPlaidTransactionIDs(ctx context.Context, userID int64, plaidTxnIDs []string) ([]model.Transaction, error) {
	if len(plaidTxnIDs) == 0 {
		return nil, nil
	}
	var out []model.Transaction
	if err := r.db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND plaid_transaction_id IN ? AND deleted_at IS NOT NULL", userID, plaidTxnIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepo) ResurrectByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error {
	// Locate the existing row Unscoped so we capture its ID + created_at
	// even though deleted_at is set. Then Save() with DeletedAt zeroed to
	// flip it back to live.
	var existing model.Transaction
	if err := r.db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
		First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	merged.ID = existing.ID
	merged.CreatedAt = existing.CreatedAt
	merged.DeletedAt = gorm.DeletedAt{} // clear soft-delete on the way out
	res := r.db.WithContext(ctx).Unscoped().
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

func (r *transactionRepo) ListForCategorizationScope(ctx context.Context, userID int64, scope string, afterID int64, limit int) ([]model.Transaction, error) {
	if limit <= 0 {
		limit = 1000
	}
	q := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Where("user_id = ?", userID)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	switch scope {
	case CategorizationScopeUncategorized:
		q = q.Where("category_id IS NULL")
	case CategorizationScopePlaidDefault:
		q = q.Where("categorization_method = ?", "plaid_default")
	case CategorizationScopeAll, "":
		// no extra predicate
	default:
		return nil, errors.New("unknown categorization scope: " + scope)
	}
	var out []model.Transaction
	if err := q.Order("id ASC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
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
