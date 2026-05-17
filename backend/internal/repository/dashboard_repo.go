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
type CategoryAggregate struct {
	CategoryID *int64
	Name       string // "Uncategorized" when CategoryID is nil
	Amount     decimal.Decimal
}

// DashboardRepository runs the read-only aggregation queries that power the
// dashboard. All queries are scoped to the requesting user_id.
type DashboardRepository interface {
	Summarize(ctx context.Context, userID int64, from, to time.Time) (*DashboardSummaryAggregates, error)
}

type dashboardRepo struct {
	db *gorm.DB
}

func NewDashboardRepository(db *gorm.DB) DashboardRepository {
	return &dashboardRepo{db: db}
}

// Summarize aggregates over [from, to) for the given user. Net worth +
// account count are NOT period-scoped (current state of the books);
// income/spending/transaction count + by-category ARE period-scoped.
func (r *dashboardRepo) Summarize(ctx context.Context, userID int64, from, to time.Time) (*DashboardSummaryAggregates, error) {
	out := &DashboardSummaryAggregates{}

	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(balance), 0)::text
		FROM accounts
		WHERE deleted_at IS NULL
		  AND user_id = ?
	`, userID).Scan(&out.NetWorth).Error; err != nil {
		return nil, err
	}

	if err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM accounts
		WHERE deleted_at IS NULL
		  AND user_id = ?
	`, userID).Scan(&out.AccountCount).Error; err != nil {
		return nil, err
	}

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
		  AND user_id = ?
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, userID, from, to).Scan(&ie).Error; err != nil {
		return nil, err
	}
	out.Income, _ = decimal.NewFromString(ie.Income)
	out.Spending, _ = decimal.NewFromString(ie.Spending)

	if err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM transactions
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, userID, from, to).Scan(&out.TransactionCount).Error; err != nil {
		return nil, err
	}

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
		  AND t.user_id = ?
		  AND t.transaction_date >= ?
		  AND t.transaction_date <  ?
		GROUP BY t.category_id, c.name
		ORDER BY ABS(COALESCE(SUM(t.amount), 0)) DESC
	`, userID, from, to).Scan(&rows).Error; err != nil {
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
