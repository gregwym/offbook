package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// DashboardSummaryAggregates is the raw aggregate output from a single
// /dashboard/summary call. All monetary values are computed in Postgres
// (NUMERIC arithmetic) and arrive in Go as shopspring/decimal so we never
// touch them as floats.
type DashboardSummaryAggregates struct {
	NetWorth         decimal.Decimal
	Income           decimal.Decimal // SUM(amount) WHERE amount > 0
	Spending         decimal.Decimal // SUM(-amount) WHERE amount < 0 — returned positive
	AccountCount     int64
	TransactionCount int64
	ByCategory       []CategoryAggregate
}

// CategoryAggregate is one row of the by-category spending breakdown.
// Amount is the period-scoped SUM(amount). Category may be nil (uncategorized).
type CategoryAggregate struct {
	CategoryID *int64
	Name       string // "Uncategorized" when CategoryID is nil
	Amount     decimal.Decimal
}

// DashboardRepository runs the read-only aggregation queries that power the
// dashboard. Kept in its own file so the queries are easy to audit and tune.
type DashboardRepository interface {
	Summarize(ctx context.Context, from, to time.Time) (*DashboardSummaryAggregates, error)
}

type dashboardRepo struct {
	db *gorm.DB
}

func NewDashboardRepository(db *gorm.DB) DashboardRepository {
	return &dashboardRepo{db: db}
}

// Summarize aggregates over [from, to). Net worth + account count are NOT
// period-scoped (they're the current state of the books); income/spending/
// transaction count + by-category breakdown ARE period-scoped.
//
// All SUMs use COALESCE so an empty set returns 0 instead of NULL.
func (r *dashboardRepo) Summarize(ctx context.Context, from, to time.Time) (*DashboardSummaryAggregates, error) {
	out := &DashboardSummaryAggregates{}

	// Net worth: sum of all non-deleted account balances. NUMERIC arithmetic.
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(balance), 0)::text
		FROM accounts
		WHERE deleted_at IS NULL
	`).Scan(&out.NetWorth).Error; err != nil {
		return nil, err
	}

	// Account count (not period-scoped).
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM accounts
		WHERE deleted_at IS NULL
	`).Scan(&out.AccountCount).Error; err != nil {
		return nil, err
	}

	// Income + spending in a single query so the table is scanned once.
	// We coerce to text so GORM hands the value to shopspring/decimal cleanly
	// rather than going through float64.
	type incExp struct {
		Income   string
		Spending string
	}
	var ie incExp
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0)::text  AS income,
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::text AS spending
		FROM transactions
		WHERE deleted_at IS NULL
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, from, to).Scan(&ie).Error; err != nil {
		return nil, err
	}
	out.Income, _ = decimal.NewFromString(ie.Income)
	out.Spending, _ = decimal.NewFromString(ie.Spending)

	// Transaction count for the period.
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM transactions
		WHERE deleted_at IS NULL
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, from, to).Scan(&out.TransactionCount).Error; err != nil {
		return nil, err
	}

	// By-category breakdown. LEFT JOIN so uncategorized rows show up as their
	// own bucket. Ordered by absolute amount descending so the biggest
	// categories appear first (charts/lists will commonly want this default).
	type rawCatRow struct {
		CategoryID *int64
		Name       *string
		Amount     string
	}
	var rows []rawCatRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			t.category_id                     AS category_id,
			c.name                            AS name,
			COALESCE(SUM(t.amount), 0)::text  AS amount
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		WHERE t.deleted_at IS NULL
		  AND t.transaction_date >= ?
		  AND t.transaction_date <  ?
		GROUP BY t.category_id, c.name
		ORDER BY ABS(COALESCE(SUM(t.amount), 0)) DESC
	`, from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out.ByCategory = make([]CategoryAggregate, 0, len(rows))
	for _, row := range rows {
		amt, _ := decimal.NewFromString(row.Amount)
		name := "Uncategorized"
		if row.Name != nil {
			name = *row.Name
		}
		out.ByCategory = append(out.ByCategory, CategoryAggregate{
			CategoryID: row.CategoryID,
			Name:       name,
			Amount:     amt,
		})
	}

	return out, nil
}
