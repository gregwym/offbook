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

// AccountRepository is the data-access contract for accounts. Every read path
// is scoped by user_id — there is no "fetch by id" without an owning user.
// PII fields live in pii_store and are NOT exposed here.
type AccountRepository interface {
	Create(ctx context.Context, a *model.Account) error
	GetByID(ctx context.Context, userID, id int64) (*model.Account, error)
	List(ctx context.Context, userID int64, f AccountFilter) ([]model.Account, int64, error)
	Update(ctx context.Context, a *model.Account) error
	SoftDelete(ctx context.Context, userID, id int64) error
	// FindByPlaidAccountID looks up a non-deleted account by its Plaid
	// account_id, scoped to userID. Returns ErrNotFound when no row exists.
	// Used by the Plaid discovery flow to make sync re-runs idempotent.
	FindByPlaidAccountID(ctx context.Context, userID int64, plaidAccountID string) (*model.Account, error)
	// ListByPlaidItemID returns every non-deleted account belonging to a
	// Plaid item, scoped to userID. Used by the sync path to reconcile each
	// account's cash position after a transactions drain — including
	// accounts that had no transactions in the current window.
	ListByPlaidItemID(ctx context.Context, userID int64, plaidItemID string) ([]model.Account, error)
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

func (r *accountRepo) GetByID(ctx context.Context, userID, id int64) (*model.Account, error) {
	var a model.Account
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&a, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *accountRepo) List(ctx context.Context, userID int64, f AccountFilter) ([]model.Account, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Account{}).Where("user_id = ?", userID)
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
	// Service layer is responsible for read-then-patch with a user-scoped read,
	// so a.UserID is guaranteed to match the session user. We still scope the
	// WHERE so a malicious mutation of a.UserID can't escape its tenant.
	res := r.db.WithContext(ctx).
		Where("user_id = ?", a.UserID).
		Save(a)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *accountRepo) FindByPlaidAccountID(ctx context.Context, userID int64, plaidAccountID string) (*model.Account, error) {
	var a model.Account
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND plaid_account_id = ?", userID, plaidAccountID).
		First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *accountRepo) ListByPlaidItemID(ctx context.Context, userID int64, plaidItemID string) ([]model.Account, error) {
	var out []model.Account
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND plaid_item_id = ?", userID, plaidItemID).
		Order("id").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *accountRepo) SoftDelete(ctx context.Context, userID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.Account{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
