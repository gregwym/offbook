package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// TransactionRepository is the data-access contract for transactions.
// List + filtering ship separately in #29 — keep this interface minimal so
// the next PR can extend it without breaking callers.
type TransactionRepository interface {
	Create(ctx context.Context, t *model.Transaction) error
	GetByID(ctx context.Context, id int64) (*model.Transaction, error)
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
