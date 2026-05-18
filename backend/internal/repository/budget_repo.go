package repository

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// BudgetRepository is the data-access contract for budgets. All read/write
// paths are scoped by user_id — budgets are user-private and never shared
// across tenants. Cross-household sharing lives in the separate
// `shared_budgets` table (deferred to M8).
type BudgetRepository interface {
	Create(ctx context.Context, b *model.Budget) error
	GetByID(ctx context.Context, userID, id int64) (*model.Budget, error)
	List(ctx context.Context, userID int64) ([]model.Budget, error)
	Update(ctx context.Context, b *model.Budget) error
	SoftDelete(ctx context.Context, userID, id int64) error
	// CurrentPeriodSpend returns the sum of -amount (i.e. positive spending)
	// over [from, to) for transactions matching (userID, categoryID). Outflows
	// only: amount < 0 in the codebase's sign convention. Returns 0 (not an
	// error) when no transactions match.
	CurrentPeriodSpend(ctx context.Context, userID, categoryID int64, from, to time.Time) (decimal.Decimal, error)
	// SpendByCategoryInRange returns SUM(-amount) FILTER (amount < 0) grouped
	// by category over [from, to), restricted to the given category_ids.
	// One query for the whole batch — avoids N round-trips when computing
	// alerts across many budgets. Categories with no matching transactions
	// are simply absent from the result map.
	SpendByCategoryInRange(ctx context.Context, userID int64, categoryIDs []int64, from, to time.Time) (map[int64]decimal.Decimal, error)
}

type budgetRepo struct {
	db *gorm.DB
}

func NewBudgetRepository(db *gorm.DB) BudgetRepository {
	return &budgetRepo{db: db}
}

func (r *budgetRepo) Create(ctx context.Context, b *model.Budget) error {
	return r.db.WithContext(ctx).Create(b).Error
}

func (r *budgetRepo) GetByID(ctx context.Context, userID, id int64) (*model.Budget, error) {
	var b model.Budget
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (r *budgetRepo) List(ctx context.Context, userID int64) ([]model.Budget, error) {
	var out []model.Budget
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_active DESC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *budgetRepo) Update(ctx context.Context, b *model.Budget) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", b.UserID).
		Save(b)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *budgetRepo) SoftDelete(ctx context.Context, userID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.Budget{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *budgetRepo) SpendByCategoryInRange(ctx context.Context, userID int64, categoryIDs []int64, from, to time.Time) (map[int64]decimal.Decimal, error) {
	if len(categoryIDs) == 0 {
		return map[int64]decimal.Decimal{}, nil
	}
	type row struct {
		CategoryID int64
		Spent      string
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			category_id                                          AS category_id,
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::text AS spent
		FROM transactions
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND category_id IN ?
		  AND is_transfer = FALSE
		  AND transaction_date >= ?
		  AND transaction_date <  ?
		GROUP BY category_id
	`, userID, categoryIDs, from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]decimal.Decimal, len(rows))
	for _, r := range rows {
		d, err := decimal.NewFromString(r.Spent)
		if err != nil {
			return nil, err
		}
		// Only positive (outflow) totals are meaningful — a category whose
		// rows are all inflows in the window returns 0 here.
		out[r.CategoryID] = d
	}
	return out, nil
}

func (r *budgetRepo) CurrentPeriodSpend(ctx context.Context, userID, categoryID int64, from, to time.Time) (decimal.Decimal, error) {
	// Sign convention: outflows are negative; we want a positive "spent"
	// total. SUM(-amount) FILTER (amount < 0) mirrors dashboard_repo.
	// Transfers are excluded so internal moves don't count as spend.
	var s string
	err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::text
		FROM transactions
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND category_id = ?
		  AND is_transfer = FALSE
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, userID, categoryID, from, to).Scan(&s).Error
	if err != nil {
		return decimal.Zero, err
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, err
	}
	return d, nil
}
