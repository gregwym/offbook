package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// AccountFilter narrows the set of accounts returned by List.
// Zero values mean "no filter on that dimension".
type AccountFilter struct {
	InstitutionSlug string
	AccountType     string
	IsActive        *bool
	Limit           int
	Offset          int
}

// AccountRepository is the data-access contract for accounts. PII fields
// (holder name, account number, etc.) live in pii_store and are NOT exposed
// here — see pii_repo for that.
type AccountRepository interface {
	Create(ctx context.Context, a *model.Account) error
	GetByID(ctx context.Context, id int64) (*model.Account, error)
	List(ctx context.Context, f AccountFilter) ([]model.Account, int64, error)
	Update(ctx context.Context, a *model.Account) error
	SoftDelete(ctx context.Context, id int64) error
}

// ErrNotFound is returned by repository methods when no matching row exists.
// Services translate this into domain-specific errors (e.g. ErrAccountNotFound).
var ErrNotFound = errors.New("not found")

type accountRepo struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) AccountRepository {
	return &accountRepo{db: db}
}

func (r *accountRepo) Create(ctx context.Context, a *model.Account) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *accountRepo) GetByID(ctx context.Context, id int64) (*model.Account, error) {
	var a model.Account
	if err := r.db.WithContext(ctx).First(&a, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *accountRepo) List(ctx context.Context, f AccountFilter) ([]model.Account, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Account{})
	if f.InstitutionSlug != "" {
		q = q.Where("institution_slug = ?", f.InstitutionSlug)
	}
	if f.AccountType != "" {
		q = q.Where("account_type = ?", f.AccountType)
	}
	if f.IsActive != nil {
		q = q.Where("is_active = ?", *f.IsActive)
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

	var out []model.Account
	if err := q.Order("id DESC").Limit(limit).Offset(f.Offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *accountRepo) Update(ctx context.Context, a *model.Account) error {
	// Save updates all fields, including the zero values that PATCH explicitly cleared
	// (e.g. unsetting LastFour). The handler is responsible for loading-then-patching
	// so we never blow away fields the caller didn't intend to change.
	res := r.db.WithContext(ctx).Save(a)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *accountRepo) SoftDelete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Account{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
