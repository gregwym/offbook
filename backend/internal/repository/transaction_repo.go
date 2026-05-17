package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

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
type TransactionRepository interface {
	Create(ctx context.Context, t *model.Transaction) error
	GetByID(ctx context.Context, id int64) (*model.Transaction, error)
	List(ctx context.Context, f TransactionFilter) ([]model.Transaction, int64, error)
	Update(ctx context.Context, t *model.Transaction) error
	SoftDelete(ctx context.Context, id int64) error
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

func (r *transactionRepo) GetByID(ctx context.Context, id int64) (*model.Transaction, error) {
	var t model.Transaction
	if err := r.db.WithContext(ctx).First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *transactionRepo) List(ctx context.Context, f TransactionFilter) ([]model.Transaction, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Transaction{})

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
		// ILIKE %term% — no full-text index in M2.
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
	res := r.db.WithContext(ctx).Save(t)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *transactionRepo) SoftDelete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Transaction{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
