package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// netWorthQuery sums positions × latest prices in the user's primary
// currency. Per ADR-0013, balance is derived from (positions, prices) —
// the legacy accounts.balance column is gone. For positions denominated
// in the user's primary currency the price is implicit (1); for others
// we look up the most-recent price into that currency. Missing-price
// fallback is 0 — calling code can surface a "stale price" warning.
const netWorthQuery = `
	SELECT COALESCE(SUM(
		CASE
			WHEN p.asset_id = u.primary_currency_asset_id THEN p.quantity
			ELSE COALESCE(
				p.quantity * (
					SELECT pr.price FROM prices pr
					WHERE pr.asset_id = p.asset_id
					  AND pr.quote_asset_id = u.primary_currency_asset_id
					ORDER BY pr.as_of DESC
					LIMIT 1
				), 0)
		END
	), 0)::text
	FROM positions p
	JOIN accounts a ON a.id = p.account_id AND a.deleted_at IS NULL
	JOIN users    u ON u.id = p.user_id
	WHERE p.deleted_at IS NULL AND p.user_id = ?`

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
	// TradeSummaryByKind aggregates security-leg counts and gross
	// notional (|security qty × latest_price_to_primary|) over [from, to),
	// grouped by the asset's kind ('equity', 'crypto', …). Fiat legs are
	// excluded by construction (a trade's security leg is never fiat).
	// Returns rows ordered by gross DESC.
	TradeSummaryByKind(ctx context.Context, userID int64, from, to time.Time) ([]TradeKindAggregate, error)
}

// TradeKindAggregate is one row of the trade rollup surfaced to the AI
// context builder. Values are user-primary-currency strings to preserve
// NUMERIC precision across the wire; the LLM sees stable decimal text.
type TradeKindAggregate struct {
	Kind       string
	LegCount   int64
	GrossValue decimal.Decimal
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

	if err := r.db.WithContext(ctx).Raw(netWorthQuery, userID).Scan(&out.NetWorth).Error; err != nil {
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
		  AND kind = 'flow'
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
		  AND t.kind = 'flow'
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
		  AND kind = 'flow'
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

// TradeSummaryByKind walks paired trade rows (security legs only, fiat
// excluded) in [from, to) and rolls them up by asset kind. Gross value
// is |security qty × latest_price_to_primary| — i.e. the security
// leg's notional expressed in the user's primary currency. Trades
// without a fresh price land in the row but contribute 0 to gross
// (same soft-fallback as net worth).
func (r *dashboardRepo) TradeSummaryByKind(ctx context.Context, userID int64, from, to time.Time) ([]TradeKindAggregate, error) {
	type row struct {
		Kind     string
		LegCount int64
		Gross    string
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			a.kind AS kind,
			COUNT(*) AS leg_count,
			COALESCE(SUM(
				ABS(t.amount) * COALESCE((
					SELECT pr.price FROM prices pr
					WHERE pr.asset_id = t.asset_id
					  AND pr.quote_asset_id = u.primary_currency_asset_id
					  AND pr.as_of <= NOW()
					ORDER BY pr.as_of DESC
					LIMIT 1
				), 0)
			), 0)::text AS gross
		FROM transactions t
		JOIN assets a ON a.id = t.asset_id
		JOIN users  u ON u.id = t.user_id
		WHERE t.deleted_at IS NULL
		  AND t.user_id = ?
		  AND t.transfer_pair_id IS NOT NULL
		  AND a.kind <> 'fiat'
		  AND t.transaction_date >= ?
		  AND t.transaction_date <  ?
		GROUP BY a.kind
		ORDER BY gross DESC
	`, userID, from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TradeKindAggregate, 0, len(rows))
	for _, r := range rows {
		gross, _ := decimal.NewFromString(r.Gross)
		out = append(out, TradeKindAggregate{Kind: r.Kind, LegCount: r.LegCount, GrossValue: gross})
	}
	return out, nil
}
