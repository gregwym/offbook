package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// AccountShareView is what the aggregator needs to apply lifecycle filtering:
// a share row joined with the owning user_id of the account. It is NOT
// surfaced through any handler — only the household aggregator consumes it.
type AccountShareView struct {
	AccountID   int64
	HouseholdID int64
	UserID      int64
	Visibility  string
}

// HouseholdAggregatorRepository is the cross-user reader that powers
// service/household/aggregator.go. By design it serves aggregated values
// (sums, counts, category rollups) and lightweight shapes — never raw
// transaction rows. It MUST NOT import pii_repo.
type HouseholdAggregatorRepository interface {
	// ListAccountShares returns active shares for the household whose visibility
	// is in `allowedVisibilities`, joined with the owning user_id of the account.
	// Soft-deleted accounts and shares are excluded.
	ListAccountShares(ctx context.Context, householdID int64, allowedVisibilities []string) ([]AccountShareView, error)

	// ListMembersIncludingLeft returns all not-yet-purged members of the
	// household (active + left-within-grace). Callers split by left_at.
	ListMembersIncludingLeft(ctx context.Context, householdID int64) ([]model.HouseholdMember, error)

	// SumBalances returns sum(balance) across the given account IDs.
	// Returns decimal.Zero on empty input — no SQL is issued in that case.
	SumBalances(ctx context.Context, accountIDs []int64) (decimal.Decimal, error)

	// PeriodIncomeSpending returns (income, spending) over [from, to)
	// across the given account IDs. Spending is returned as a positive value.
	PeriodIncomeSpending(ctx context.Context, accountIDs []int64, from, to time.Time) (decimal.Decimal, decimal.Decimal, error)

	// PeriodTransactionCount counts non-deleted transactions in [from, to)
	// across the given account IDs.
	PeriodTransactionCount(ctx context.Context, accountIDs []int64, from, to time.Time) (int64, error)

	// CategoryAggregates rolls up SUM(amount) by category over [from, to).
	// `LEFT JOIN categories` so uncategorized rows are bucketed as "Uncategorized".
	CategoryAggregates(ctx context.Context, accountIDs []int64, from, to time.Time) ([]CategoryAggregate, error)

	// SpendingByCategory returns SUM(-amount) FILTER (amount < 0) for one
	// category over [from, to) across the given account IDs. Returns a
	// positive number (spending). Used by BudgetPace.
	SpendingByCategory(ctx context.Context, accountIDs []int64, categoryID int64, from, to time.Time) (decimal.Decimal, error)

	// ListSharedBudgets returns shared_budgets for the household (optionally
	// filtered by period). M2.5 ships no CRUD for these — readers tolerate
	// an empty result set.
	ListSharedBudgets(ctx context.Context, householdID int64, period string) ([]model.SharedBudget, error)

	// ListSharedGoals returns shared_goals for the household.
	ListSharedGoals(ctx context.Context, householdID int64) ([]model.SharedGoal, error)

	// ListSharedThreads returns ai_threads where shared_with_household = true
	// and household_id matches. Order: most recently updated first.
	ListSharedThreads(ctx context.Context, householdID int64, limit int) ([]model.AIThread, error)

	// ListPersonalThreadsForUser returns the user's own threads NOT shared
	// with any household. Order: most recently updated first.
	ListPersonalThreadsForUser(ctx context.Context, userID int64, limit int) ([]model.AIThread, error)

	// AllocationByKind sums positions × latest prices across the given
	// account IDs, grouped by assets.kind, in each position owner's
	// primary currency. Empty accountIDs returns nil.
	AllocationByKind(ctx context.Context, accountIDs []int64) ([]AllocationBucket, error)

	// NetWorthSeries returns one daily net-worth point per day in
	// [from, to] (inclusive), summing CURRENT positions × historical
	// prices across accountIDs. Days with no fresh price for a position
	// contribute 0 for that position — matches the soft-fallback behavior
	// of the live dashboard query so the chart is renderable even when a
	// pricing gap exists.
	NetWorthSeries(ctx context.Context, accountIDs []int64, from, to time.Time) ([]NetWorthPoint, error)

	// AccountBalances returns one row per account in accountIDs with the
	// account's current balance in its owner's primary currency. Missing
	// account IDs (e.g. soft-deleted) are silently omitted.
	AccountBalances(ctx context.Context, accountIDs []int64) ([]AccountBalanceRow, error)
}

// AllocationBucket is one (asset_kind, value) row of the household
// allocation rollup. Value is in each position owner's primary currency
// — heterogeneous-currency households mix into the owner's preferred
// quote per position.
type AllocationBucket struct {
	Kind  string
	Value decimal.Decimal
}

// NetWorthPoint is one (date, value) point of the household net-worth
// trend. Date is the calendar day (UTC midnight); Value is the sum of
// current positions × historical prices for that day.
type NetWorthPoint struct {
	Date  time.Time
	Value decimal.Decimal
}

// AccountBalanceRow is the per-account balance contribution from
// AccountBalances — minimal projection so handlers can join with the
// share view to add visibility and owner info.
type AccountBalanceRow struct {
	AccountID   int64
	Name        string
	AccountType string
	Currency    string
	OwnerUserID int64
	Balance     decimal.Decimal
}

type householdAggregatorRepo struct{ db *gorm.DB }

func NewHouseholdAggregatorRepository(db *gorm.DB) HouseholdAggregatorRepository {
	return &householdAggregatorRepo{db: db}
}

func (r *householdAggregatorRepo) ListAccountShares(ctx context.Context, householdID int64, allowed []string) ([]AccountShareView, error) {
	if len(allowed) == 0 {
		return nil, nil
	}
	var rows []AccountShareView
	err := r.db.WithContext(ctx).Raw(`
		SELECT s.account_id   AS account_id,
		       s.household_id AS household_id,
		       a.user_id      AS user_id,
		       s.visibility   AS visibility
		FROM account_shares s
		JOIN accounts a ON a.id = s.account_id
		WHERE s.deleted_at IS NULL
		  AND a.deleted_at IS NULL
		  AND s.household_id = ?
		  AND s.visibility IN ?
	`, householdID, allowed).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *householdAggregatorRepo) ListMembersIncludingLeft(ctx context.Context, householdID int64) ([]model.HouseholdMember, error) {
	var out []model.HouseholdMember
	err := r.db.WithContext(ctx).
		Where("household_id = ? AND purged_at IS NULL", householdID).
		Order("CASE role WHEN 'owner' THEN 0 WHEN 'contributor' THEN 1 ELSE 2 END, id").
		Find(&out).Error
	return out, err
}

func (r *householdAggregatorRepo) SumBalances(ctx context.Context, accountIDs []int64) (decimal.Decimal, error) {
	if len(accountIDs) == 0 {
		return decimal.Zero, nil
	}
	// Per ADR-0013, balance is derived from positions × latest prices —
	// the legacy accounts.balance column is gone. For positions denominated
	// in the position owner's primary currency the price is implicit (1);
	// for others we look up the most-recent price into that currency.
	var s string
	err := r.db.WithContext(ctx).Raw(`
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
		WHERE p.deleted_at IS NULL AND p.account_id IN ?
	`, accountIDs).Scan(&s).Error
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

func (r *householdAggregatorRepo) PeriodIncomeSpending(ctx context.Context, accountIDs []int64, from, to time.Time) (decimal.Decimal, decimal.Decimal, error) {
	if len(accountIDs) == 0 {
		return decimal.Zero, decimal.Zero, nil
	}
	type incExp struct {
		Income   string
		Spending string
	}
	var ie incExp
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(SUM(amount)  FILTER (WHERE amount > 0), 0)::text  AS income,
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::text  AS spending
		FROM transactions
		WHERE deleted_at IS NULL
		  AND account_id IN ?
		  AND kind NOT IN ('opening_balance', 'adjustment')
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, accountIDs, from, to).Scan(&ie).Error
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	inc, _ := decimal.NewFromString(ie.Income)
	sp, _ := decimal.NewFromString(ie.Spending)
	return inc, sp, nil
}

func (r *householdAggregatorRepo) PeriodTransactionCount(ctx context.Context, accountIDs []int64, from, to time.Time) (int64, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM transactions
		WHERE deleted_at IS NULL
		  AND account_id IN ?
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, accountIDs, from, to).Scan(&n).Error
	return n, err
}

func (r *householdAggregatorRepo) CategoryAggregates(ctx context.Context, accountIDs []int64, from, to time.Time) ([]CategoryAggregate, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	type rawRow struct {
		CategoryID *int64
		Name       *string
		Amount     string
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT t.category_id                    AS category_id,
		       c.name                           AS name,
		       COALESCE(SUM(t.amount), 0)::text AS amount
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		WHERE t.deleted_at IS NULL
		  AND t.account_id IN ?
		  AND t.kind NOT IN ('opening_balance', 'adjustment')
		  AND t.transaction_date >= ?
		  AND t.transaction_date <  ?
		GROUP BY t.category_id, c.name
		ORDER BY ABS(COALESCE(SUM(t.amount), 0)) DESC
	`, accountIDs, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]CategoryAggregate, 0, len(rows))
	for _, row := range rows {
		amt, _ := decimal.NewFromString(row.Amount)
		name := "Uncategorized"
		if row.Name != nil {
			name = *row.Name
		}
		out = append(out, CategoryAggregate{
			CategoryID: row.CategoryID,
			Name:       name,
			Amount:     amt,
		})
	}
	return out, nil
}

func (r *householdAggregatorRepo) SpendingByCategory(ctx context.Context, accountIDs []int64, categoryID int64, from, to time.Time) (decimal.Decimal, error) {
	if len(accountIDs) == 0 {
		return decimal.Zero, nil
	}
	var s string
	err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::text
		FROM transactions
		WHERE deleted_at IS NULL
		  AND account_id IN ?
		  AND category_id = ?
		  AND kind NOT IN ('opening_balance', 'adjustment')
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, accountIDs, categoryID, from, to).Scan(&s).Error
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

func (r *householdAggregatorRepo) ListSharedBudgets(ctx context.Context, householdID int64, period string) ([]model.SharedBudget, error) {
	q := r.db.WithContext(ctx).Where("household_id = ? AND is_active = ?", householdID, true)
	if period != "" {
		q = q.Where("period = ?", period)
	}
	var out []model.SharedBudget
	if err := q.Order("id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *householdAggregatorRepo) ListSharedGoals(ctx context.Context, householdID int64) ([]model.SharedGoal, error) {
	var out []model.SharedGoal
	err := r.db.WithContext(ctx).
		Where("household_id = ?", householdID).
		Order("id").
		Find(&out).Error
	return out, err
}

func (r *householdAggregatorRepo) ListSharedThreads(ctx context.Context, householdID int64, limit int) ([]model.AIThread, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []model.AIThread
	err := r.db.WithContext(ctx).
		Where("household_id = ? AND shared_with_household = ?", householdID, true).
		Order("updated_at DESC, id DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// positionValueExpr is the SQL fragment that prices one position into its
// owner's primary currency. Shared between AllocationByKind, NetWorthSeries,
// and AccountBalances so a future price-source change touches one spot.
// References: positions p, users u, prices pr. For NetWorthSeries the
// trailing as_of bound is replaced via the asOfBound parameter.
const positionValueExpr = `
		CASE
			WHEN p.asset_id = u.primary_currency_asset_id THEN p.quantity
			ELSE COALESCE(
				p.quantity * (
					SELECT pr.price FROM prices pr
					WHERE pr.asset_id = p.asset_id
					  AND pr.quote_asset_id = u.primary_currency_asset_id
					  %s
					ORDER BY pr.as_of DESC
					LIMIT 1
				), 0)
		END`

func (r *householdAggregatorRepo) AllocationByKind(ctx context.Context, accountIDs []int64) ([]AllocationBucket, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	type rawRow struct {
		Kind  string
		Value string
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT ass.kind AS kind,
		       COALESCE(SUM(`+fmt.Sprintf(positionValueExpr, "")+`), 0)::text AS value
		FROM positions p
		JOIN accounts a ON a.id = p.account_id AND a.deleted_at IS NULL
		JOIN users    u ON u.id = p.user_id
		JOIN assets   ass ON ass.id = p.asset_id
		WHERE p.deleted_at IS NULL AND p.account_id IN ?
		GROUP BY ass.kind
		ORDER BY ass.kind
	`, accountIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]AllocationBucket, 0, len(rows))
	for _, row := range rows {
		v, _ := decimal.NewFromString(row.Value)
		out = append(out, AllocationBucket{Kind: row.Kind, Value: v})
	}
	return out, nil
}

func (r *householdAggregatorRepo) NetWorthSeries(ctx context.Context, accountIDs []int64, from, to time.Time) ([]NetWorthPoint, error) {
	if len(accountIDs) == 0 || !to.After(from.Add(-time.Second)) {
		return nil, nil
	}
	fromDay := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toDay := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	type rawRow struct {
		Date  time.Time
		Value string
	}
	var rows []rawRow
	// Cross-join generate_series with positions; price lookup is bounded by
	// `d + 1 day` so a day's entry uses the latest price posted ON OR BEFORE
	// end-of-that-day. LEFT JOIN keeps days with no positions at 0.
	err := r.db.WithContext(ctx).Raw(`
		WITH days AS (
			SELECT generate_series(?::date, ?::date, interval '1 day')::date AS d
		)
		SELECT days.d AS date,
		       COALESCE(SUM(`+fmt.Sprintf(positionValueExpr, "AND pr.as_of <= days.d + interval '1 day'")+`), 0)::text AS value
		FROM days
		LEFT JOIN positions p ON p.deleted_at IS NULL AND p.account_id IN ?
		LEFT JOIN accounts  a ON a.id = p.account_id AND a.deleted_at IS NULL
		LEFT JOIN users     u ON u.id = p.user_id
		GROUP BY days.d
		ORDER BY days.d
	`, fromDay, toDay, accountIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]NetWorthPoint, 0, len(rows))
	for _, row := range rows {
		v, _ := decimal.NewFromString(row.Value)
		out = append(out, NetWorthPoint{Date: row.Date.UTC(), Value: v})
	}
	return out, nil
}

func (r *householdAggregatorRepo) AccountBalances(ctx context.Context, accountIDs []int64) ([]AccountBalanceRow, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	type rawRow struct {
		AccountID   int64
		Name        string
		AccountType string
		Currency    string
		OwnerUserID int64
		Balance     string
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT a.id           AS account_id,
		       a.name         AS name,
		       a.account_type AS account_type,
		       qa.symbol      AS currency,
		       a.user_id      AS owner_user_id,
		       COALESCE(SUM(`+fmt.Sprintf(positionValueExpr, "")+`), 0)::text AS balance
		FROM accounts a
		JOIN users     u  ON u.id = a.user_id
		JOIN assets    qa ON qa.id = a.primary_quote_asset_id
		LEFT JOIN positions p ON p.deleted_at IS NULL AND p.account_id = a.id
		WHERE a.deleted_at IS NULL AND a.id IN ?
		GROUP BY a.id, a.name, a.account_type, qa.symbol, a.user_id
		ORDER BY a.id
	`, accountIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]AccountBalanceRow, 0, len(rows))
	for _, row := range rows {
		b, _ := decimal.NewFromString(row.Balance)
		out = append(out, AccountBalanceRow{
			AccountID:   row.AccountID,
			Name:        row.Name,
			AccountType: row.AccountType,
			Currency:    row.Currency,
			OwnerUserID: row.OwnerUserID,
			Balance:     b,
		})
	}
	return out, nil
}

func (r *householdAggregatorRepo) ListPersonalThreadsForUser(ctx context.Context, userID int64, limit int) ([]model.AIThread, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []model.AIThread
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND shared_with_household = ?", userID, false).
		Order("updated_at DESC, id DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}
