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

// CategorySpendItem is one row of the spend-by-category chart, enriched
// with the category's color so the frontend pie slices stay consistent
// with the budget bars.
type CategorySpendItem struct {
	CategoryID *int64
	Name       string // "Uncategorized" when CategoryID is nil
	Color      string // empty string when not set
	Amount     decimal.Decimal
}

// CashFlowMonth is one row of the monthly cash-flow chart. Month is the
// first-of-month timestamp (UTC). Inflow + Outflow are both positive;
// Net = Inflow - Outflow.
type CashFlowMonth struct {
	Month   time.Time
	Inflow  decimal.Decimal
	Outflow decimal.Decimal
	Net     decimal.Decimal
}

// NetWorthMonth is one row of the net-worth trend chart. Date is the
// month-end day (e.g. 2026-05-31 for May 2026). Total is the
// back-derived account-balance total at that point.
type NetWorthMonth struct {
	Date  time.Time
	Total decimal.Decimal
}

// DashboardRepository runs the read-only aggregation queries that power the
// dashboard. All queries are scoped to the requesting user_id.
type DashboardRepository interface {
	Summarize(ctx context.Context, userID int64, from, to time.Time) (*DashboardSummaryAggregates, error)
	// SpendByCategory returns SUM(-amount) FILTER (amount < 0) grouped by
	// category over [from, to). Outflows only — inflows excluded. Transfers
	// excluded (internal moves are not spending). Ordered by amount DESC.
	SpendByCategory(ctx context.Context, userID int64, from, to time.Time) ([]CategorySpendItem, error)
	// CashFlowByMonth returns one row per month for the last `months`
	// calendar months ending at the month containing `now`. Income/outflow
	// are positive; transfers excluded.
	CashFlowByMonth(ctx context.Context, userID int64, now time.Time, months int) ([]CashFlowMonth, error)
	// NetWorthByMonth returns approximated month-end account totals for the
	// last `months` months ending at `now`. The current month's row uses
	// the current persisted balance; older months back-derive by undoing
	// transactions between the row's month-end and now.
	NetWorthByMonth(ctx context.Context, userID int64, now time.Time, months int) ([]NetWorthMonth, error)
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

// SpendByCategory: outflows only, transfers excluded, grouped + sorted.
func (r *dashboardRepo) SpendByCategory(ctx context.Context, userID int64, from, to time.Time) ([]CategorySpendItem, error) {
	type rawRow struct {
		CategoryID *int64
		Name       *string
		Color      *string
		Amount     string
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			t.category_id                                       AS category_id,
			c.name                                              AS name,
			c.color                                             AS color,
			COALESCE(SUM(-t.amount), 0)::text                    AS amount
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		WHERE t.deleted_at IS NULL
		  AND t.user_id = ?
		  AND t.is_transfer = FALSE
		  AND t.amount < 0
		  AND t.transaction_date >= ?
		  AND t.transaction_date <  ?
		GROUP BY t.category_id, c.name, c.color
		ORDER BY SUM(-t.amount) DESC
	`, userID, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]CategorySpendItem, 0, len(rows))
	for _, row := range rows {
		amt, _ := decimal.NewFromString(row.Amount)
		name := "Uncategorized"
		if row.Name != nil {
			name = *row.Name
		}
		color := ""
		if row.Color != nil {
			color = *row.Color
		}
		out = append(out, CategorySpendItem{
			CategoryID: row.CategoryID,
			Name:       name,
			Color:      color,
			Amount:     amt,
		})
	}
	return out, nil
}

// CashFlowByMonth: bucket transactions by month for the trailing `months`
// calendar months. Inflow = SUM(amount) WHERE amount > 0; Outflow =
// SUM(-amount) WHERE amount < 0; both positive. Transfers excluded.
//
// We pre-build the month skeleton in Go so empty months still appear with
// zeros — a chart that drops a flat month is a UX regression.
func (r *dashboardRepo) CashFlowByMonth(ctx context.Context, userID int64, now time.Time, months int) ([]CashFlowMonth, error) {
	if months <= 0 {
		months = 12
	}
	// First-of-current-month in UTC; window starts (months-1) months earlier.
	now = now.UTC()
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	start := end.AddDate(0, -months, 0)

	type rawRow struct {
		Month   time.Time
		Inflow  string
		Outflow string
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			date_trunc('month', transaction_date)::timestamptz   AS month,
			COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0)::text   AS inflow,
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::text  AS outflow
		FROM transactions
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND is_transfer = FALSE
		  AND transaction_date >= ?
		  AND transaction_date <  ?
		GROUP BY date_trunc('month', transaction_date)
	`, userID, start, end).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	byMonth := make(map[time.Time]CashFlowMonth, len(rows))
	for _, row := range rows {
		in, _ := decimal.NewFromString(row.Inflow)
		out, _ := decimal.NewFromString(row.Outflow)
		byMonth[row.Month.UTC()] = CashFlowMonth{
			Month: row.Month.UTC(), Inflow: in, Outflow: out, Net: in.Sub(out),
		}
	}
	out := make([]CashFlowMonth, 0, months)
	for i := 0; i < months; i++ {
		m := start.AddDate(0, i, 0)
		if r, ok := byMonth[m]; ok {
			out = append(out, r)
		} else {
			out = append(out, CashFlowMonth{Month: m, Inflow: decimal.Zero, Outflow: decimal.Zero, Net: decimal.Zero})
		}
	}
	return out, nil
}

// NetWorthByMonth approximates month-end balances by walking backwards
// from the current persisted total. Methodology: today's net worth is the
// sum of live accounts.balance. Subtract all transactions dated after
// month-end to back-derive the balance at month-end.
//
// This is an approximation — non-transactional balance changes (manual
// edits to accounts.balance, currency conversion gaps, etc.) won't be
// reflected. Documented in the chart caption. A future enhancement could
// snapshot daily balances; for M5 this gets us a trendline.
func (r *dashboardRepo) NetWorthByMonth(ctx context.Context, userID int64, now time.Time, months int) ([]NetWorthMonth, error) {
	if months <= 0 {
		months = 12
	}
	now = now.UTC()
	// Current net worth.
	var cur string
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(balance), 0)::text
		FROM accounts
		WHERE deleted_at IS NULL
		  AND user_id = ?
	`, userID).Scan(&cur).Error; err != nil {
		return nil, err
	}
	curD, _ := decimal.NewFromString(cur)

	// Month boundaries: monthEnd[i] is the last day of the i-th month
	// (oldest first). For "last 12 months ending now", the newest row is
	// the current month-end (today's date capped at month-end).
	startMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -months+1, 0)
	out := make([]NetWorthMonth, 0, months)
	for i := 0; i < months; i++ {
		first := startMonth.AddDate(0, i, 0)
		// Day before next month-first = month-end.
		monthEnd := first.AddDate(0, 1, 0).AddDate(0, 0, -1)
		out = append(out, NetWorthMonth{Date: monthEnd})
	}

	// For every month, sum transactions with transaction_date > monthEnd
	// → those are the moves that happened between that point and now.
	// Subtract from current total. We issue ONE query that fetches all
	// transactions newer than the oldest month boundary, then bucket in Go.
	type txRow struct {
		Date   time.Time
		Amount string
	}
	var txns []txRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT transaction_date AS date, amount::text AS amount
		FROM transactions
		WHERE deleted_at IS NULL
		  AND user_id = ?
		  AND transaction_date > ?
	`, userID, out[0].Date).Scan(&txns).Error; err != nil {
		return nil, err
	}

	// Compute cumulative "since" sum per boundary by sorting boundaries and
	// transactions, walking through. For len(out) ≤ 24 a quadratic loop
	// is fine; not bothering to optimize.
	for i := range out {
		boundary := out[i].Date
		undo := decimal.Zero
		for _, t := range txns {
			if t.Date.UTC().After(boundary) {
				amt, _ := decimal.NewFromString(t.Amount)
				undo = undo.Add(amt)
			}
		}
		out[i].Total = curD.Sub(undo)
	}
	return out, nil
}
