package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// BudgetRepository is the data-access contract for budgets. CRUD is scoped by
// PlanOwner — a personal book (user_id) or a household (household_id), per
// ADR-0018. Spend computation stays user-scoped: only the personal evaluation
// path uses it; household spend is computed by the aggregator.
type BudgetRepository interface {
	// Create inserts the budget and its first limit-history row, effective
	// from amountFrom, in one transaction (#580).
	Create(ctx context.Context, b *model.Budget, amountFrom time.Time) error
	GetByID(ctx context.Context, owner PlanOwner, id int64) (*model.Budget, error)
	List(ctx context.Context, owner PlanOwner) ([]model.Budget, error)
	// Update saves the budget. A non-nil change also records the new limit
	// in the limit history, in the same transaction (#580).
	Update(ctx context.Context, b *model.Budget, change *BudgetLimitChange) error
	// AmountHistory returns a budget's limit history, oldest first. Callers
	// must have resolved the budget through an owner-scoped read.
	AmountHistory(ctx context.Context, budgetID int64) ([]model.BudgetAmount, error)
	SoftDelete(ctx context.Context, owner PlanOwner, id int64) error
	// CurrentPeriodSpend returns the sum of -amount (i.e. positive spending)
	// over [from, to) for the user's transactions in any of categoryIDs — a
	// budget's covered categories (#596). Outflows only: amount < 0 in the
	// codebase's sign convention. Returns 0 (not an error) when no
	// transactions match.
	CurrentPeriodSpend(ctx context.Context, userID int64, categoryIDs []int64, from, to time.Time) (decimal.Decimal, error)
	// SpendByCategoryInRange returns SUM(-amount) FILTER (amount < 0) grouped
	// by category over [from, to), restricted to the given category_ids.
	// One query for the whole batch — avoids N round-trips when computing
	// alerts across many budgets. Categories with no matching transactions
	// are simply absent from the result map.
	SpendByCategoryInRange(ctx context.Context, userID int64, categoryIDs []int64, from, to time.Time) (map[int64]decimal.Decimal, error)
	// SpendByPeriod returns the spend of categoryIDs (same predicate as
	// CurrentPeriodSpend) bucketed by calendar period ("month", "week" or
	// "year", Monday-start weeks) over [from, to). Keys are the UTC bucket
	// start dates; periods with no spend are absent. Used for rollover.
	SpendByPeriod(ctx context.Context, userID int64, categoryIDs []int64, unit string, from, to time.Time) (map[time.Time]decimal.Decimal, error)
	// SpendAllCategoriesInRange is the month's spending for every category
	// (nil = uncategorized) over [from, to), with the same predicate as
	// CurrentPeriodSpend and the Insights spending total — so its rows sum to
	// that total exactly (#589). Categories with no spend are absent.
	SpendAllCategoriesInRange(ctx context.Context, userID int64, from, to time.Time) ([]CategorySpend, error)
}

// CategorySpend is one category's spending; CategoryID nil is uncategorized.
type CategorySpend struct {
	CategoryID *int64
	Spent      decimal.Decimal
}

// BudgetLimitChange records a limit edit (#580): b.Amount applies from From
// (the current period start). Earlier periods keep Prev, the limit before the
// edit: when no history row predates From, Prev is pinned at PrevFrom (the
// period before) so an edit in the creation period doesn't rewrite the months
// the user already looked at.
type BudgetLimitChange struct {
	From     time.Time
	PrevFrom time.Time
	Prev     decimal.Decimal
}

type budgetRepo struct {
	db *gorm.DB
}

func NewBudgetRepository(db *gorm.DB) BudgetRepository {
	return &budgetRepo{db: db}
}

func (r *budgetRepo) Create(ctx context.Context, b *model.Budget, amountFrom time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(b).Error; err != nil {
			return err
		}
		return upsertBudgetAmount(tx, b.ID, b.Amount, amountFrom)
	})
}

// upsertBudgetAmount records amount as the limit from `from`; a second edit
// in the same period replaces the first.
func upsertBudgetAmount(tx *gorm.DB, budgetID int64, amount decimal.Decimal, from time.Time) error {
	return tx.Exec(`INSERT INTO budget_amounts (budget_id, amount, effective_from)
		VALUES (?, ?, ?)
		ON CONFLICT (budget_id, effective_from)
		DO UPDATE SET amount = EXCLUDED.amount, updated_at = now()`,
		budgetID, amount, from.UTC().Format("2006-01-02")).Error
}

func (r *budgetRepo) AmountHistory(ctx context.Context, budgetID int64) ([]model.BudgetAmount, error) {
	var out []model.BudgetAmount
	if err := r.db.WithContext(ctx).
		Where("budget_id = ?", budgetID).
		Order("effective_from ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *budgetRepo) GetByID(ctx context.Context, owner PlanOwner, id int64) (*model.Budget, error) {
	var b model.Budget
	if err := owner.Apply(r.db.WithContext(ctx)).
		First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (r *budgetRepo) List(ctx context.Context, owner PlanOwner) ([]model.Budget, error) {
	var out []model.Budget
	if err := owner.Apply(r.db.WithContext(ctx)).
		Order("is_active DESC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *budgetRepo) Update(ctx context.Context, b *model.Budget, change *BudgetLimitChange) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := PlanOwner{UserID: b.UserID, HouseholdID: b.HouseholdID}.
			Apply(tx).
			Save(b)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if change == nil {
			return nil
		}
		if err := tx.Exec(`INSERT INTO budget_amounts (budget_id, amount, effective_from)
			SELECT ?, ?, ?
			 WHERE NOT EXISTS (SELECT 1 FROM budget_amounts WHERE budget_id = ? AND effective_from < ?)`,
			b.ID, change.Prev, change.PrevFrom.UTC().Format("2006-01-02"),
			b.ID, change.From.UTC().Format("2006-01-02")).Error; err != nil {
			return err
		}
		return upsertBudgetAmount(tx, b.ID, b.Amount, change.From)
	})
}

func (r *budgetRepo) SoftDelete(ctx context.Context, owner PlanOwner, id int64) error {
	res := owner.Apply(r.db.WithContext(ctx)).
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
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0 OR is_refund), 0)::text AS spent
		FROM spend_lines
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND category_id IN ?
		  AND is_transfer = FALSE
		  AND kind = 'flow'
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

func (r *budgetRepo) CurrentPeriodSpend(ctx context.Context, userID int64, categoryIDs []int64, from, to time.Time) (decimal.Decimal, error) {
	// Sign convention: outflows are negative; we want a positive "spent"
	// total. SUM(-amount) FILTER (amount < 0) mirrors dashboard_repo.
	// Transfers are excluded so internal moves don't count as spend.
	var s string
	err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(-amount) FILTER (WHERE amount < 0 OR is_refund), 0)::text
		FROM spend_lines
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND category_id IN ?
		  AND is_transfer = FALSE
		  AND kind = 'flow'
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, userID, categoryIDs, from, to).Scan(&s).Error
	if err != nil {
		return decimal.Zero, err
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, err
	}
	return d, nil
}

func (r *budgetRepo) SpendByPeriod(ctx context.Context, userID int64, categoryIDs []int64, unit string, from, to time.Time) (map[time.Time]decimal.Decimal, error) {
	switch unit {
	case "month", "week", "year":
	default:
		return nil, fmt.Errorf("budget spend: invalid bucket unit %q", unit)
	}
	type row struct {
		Bucket time.Time
		Spent  string
	}
	var rows []row
	// unit is whitelisted above, so interpolating it is safe.
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			date_trunc('`+unit+`', transaction_date::timestamp)::date AS bucket,
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0 OR is_refund), 0)::text AS spent
		FROM spend_lines
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND category_id IN ?
		  AND is_transfer = FALSE
		  AND kind = 'flow'
		  AND transaction_date >= ?
		  AND transaction_date <  ?
		GROUP BY 1
	`, userID, categoryIDs, from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[time.Time]decimal.Decimal, len(rows))
	for _, r := range rows {
		d, err := decimal.NewFromString(r.Spent)
		if err != nil {
			return nil, err
		}
		b := r.Bucket.UTC()
		out[time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)] = d
	}
	return out, nil
}

func (r *budgetRepo) SpendAllCategoriesInRange(ctx context.Context, userID int64, from, to time.Time) ([]CategorySpend, error) {
	type row struct {
		CategoryID *int64
		Spent      string
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			category_id                                          AS category_id,
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0 OR is_refund), 0)::text AS spent
		FROM spend_lines
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND is_transfer = FALSE
		  AND kind = 'flow'
		  AND transaction_date >= ?
		  AND transaction_date <  ?
		GROUP BY category_id
		ORDER BY SUM(-amount) FILTER (WHERE amount < 0 OR is_refund) DESC
	`, userID, from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CategorySpend, 0, len(rows))
	for _, r := range rows {
		d, err := decimal.NewFromString(r.Spent)
		if err != nil {
			return nil, err
		}
		if d.IsZero() {
			continue
		}
		out = append(out, CategorySpend{CategoryID: r.CategoryID, Spent: d})
	}
	return out, nil
}
