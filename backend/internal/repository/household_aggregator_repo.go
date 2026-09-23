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

	// SumBalances returns sum(balance) across the given account IDs, plus
	// a completeness flag: false when any position in the set had no price
	// row at-or-after freshAfter (and isn't denominated in its owner's
	// primary currency), i.e. the sum may rest on stale rates (#344).
	// Returns (decimal.Zero, true) on empty input — no SQL is issued.
	SumBalances(ctx context.Context, accountIDs []int64, freshAfter time.Time) (decimal.Decimal, bool, error)

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

	// ListSharedBudgets returns the household-owned budgets (optionally
	// filtered by period). Since ADR-0018 these are rows in the unified
	// `budgets` table with household_id set; readers tolerate an empty set.
	ListSharedBudgets(ctx context.Context, householdID int64, period string) ([]model.Budget, error)

	// ListSharedGoals returns the household-owned savings goals (unified
	// `savings_goals` table, household_id set).
	ListSharedGoals(ctx context.Context, householdID int64) ([]model.SavingsGoal, error)

	// ListSharedThreads returns ai_threads where shared_with_household = true
	// and household_id matches. Order: most recently updated first.
	ListSharedThreads(ctx context.Context, householdID int64, limit int) ([]model.AIThread, error)

	// ListPersonalThreadsForUser returns the user's own threads NOT shared
	// with any household. Order: most recently updated first.
	ListPersonalThreadsForUser(ctx context.Context, userID int64, limit int) ([]model.AIThread, error)

	// ListPositionsForAllocation returns every live position across the given
	// account set with its asset kind, for the household allocation rollup.
	// Pricing is NOT done in SQL — the caller values each position through the
	// shared valuation derivation so an unpriced asset is surfaced as
	// incomplete rather than silently $0 (#282). Empty accountIDs returns nil.
	ListPositionsForAllocation(ctx context.Context, accountIDs []int64) ([]AllocationPosition, error)

	// FoldByAccountSetAsOf returns one synthetic position per asset across the
	// given account set: quantity = Σ non-deleted transactions.amount with
	// transaction_date <= asOf. Assets folding to zero are omitted. This is the
	// cross-user dated fold behind the unified household net-worth trend (#282)
	// — the household analogue of TransactionRepository.FoldByUserAsOf, but
	// scoped by account set rather than user (the aggregator is the only
	// blessed cross-user reader). AccountID/UserID are left zero; the caller
	// values each asset in a single household quote currency.
	FoldByAccountSetAsOf(ctx context.Context, accountIDs []int64, asOf time.Time) ([]model.Position, error)

	// HouseholdQuoteAssetID returns the asset_id the household's aggregates are
	// denominated in: the owning member's primary_currency_asset_id. A single
	// quote per household keeps the net-worth trend from summing across
	// heterogeneous currencies.
	HouseholdQuoteAssetID(ctx context.Context, householdID int64) (int64, error)

	// AccountBalances returns one row per account in accountIDs with the
	// account's current balance in its owner's primary currency. Missing
	// account IDs (e.g. soft-deleted) are silently omitted. freshAfter is
	// the staleness cutoff for the Complete flag: a position with no price
	// row at-or-after it (and not denominated in the owner's primary
	// currency) marks the account incomplete (#339).
	AccountBalances(ctx context.Context, accountIDs []int64, freshAfter time.Time) ([]AccountBalanceRow, error)

	// CategoryTrend mirrors DashboardRepository.CategoryTrend, scoped to the
	// given account set instead of a single user (#367): SUM(-amount)
	// FILTER (amount < 0) grouped by (category, month) over the trailing
	// `months` calendar months ending at the month containing `now`. Same
	// flow + transfer-excluded filter. Empty accountIDs returns nil.
	CategoryTrend(ctx context.Context, accountIDs []int64, now time.Time, months int) ([]CategoryMonthAmount, error)

	// TopMerchants mirrors DashboardRepository.TopMerchants, scoped to the
	// given account set (#367). Empty accountIDs returns nil.
	TopMerchants(ctx context.Context, accountIDs []int64, from, to time.Time, limit int) ([]MerchantSpendItem, error)

	// CashFlowByMonth mirrors DashboardRepository.CashFlowByMonth, scoped to
	// the given account set — income vs. spending trend by month (#367).
	// Empty accountIDs returns nil.
	CashFlowByMonth(ctx context.Context, accountIDs []int64, now time.Time, months int) ([]CashFlowMonth, error)
}

// AllocationPosition is one live position plus its asset kind, fed to the
// valuation derivation for the household allocation rollup. AssetID + Quantity
// are what valuation.Value needs; Kind buckets the result.
type AllocationPosition struct {
	AssetID  int64
	Quantity decimal.Decimal
	Kind     string
}

// AccountBalanceRow is the per-account balance contribution from
// AccountBalances — minimal projection so handlers can join with the
// share view to add visibility and owner info. Complete is false when
// any position in the account had no price row observed at-or-after the
// caller's freshness cutoff (#339): Balance may still include a stale
// valuation, but the row is flagged so the UI can render it as partial
// rather than confidently current.
type AccountBalanceRow struct {
	AccountID   int64
	Name        string
	AccountType string
	Currency    string
	OwnerUserID int64
	Balance     decimal.Decimal
	Complete    bool
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

func (r *householdAggregatorRepo) SumBalances(ctx context.Context, accountIDs []int64, freshAfter time.Time) (decimal.Decimal, bool, error) {
	if len(accountIDs) == 0 {
		return decimal.Zero, true, nil
	}
	// Per ADR-0013, balance is derived from positions × latest prices —
	// the legacy accounts.balance column is gone. For positions denominated
	// in the position owner's primary currency the price is implicit (1);
	// for others we look up the most-recent price into that currency. The
	// complete flag mirrors AccountBalances (#339/#344): any position whose
	// freshest price predates freshAfter marks the sum partial.
	type rawRow struct {
		Balance  string
		Complete bool
	}
	var row rawRow
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
		), 0)::text AS balance,
		COALESCE(BOOL_AND(
			p.asset_id = u.primary_currency_asset_id
			OR EXISTS (
				SELECT 1 FROM prices pr
				WHERE pr.asset_id = p.asset_id
				  AND pr.quote_asset_id = u.primary_currency_asset_id
				  AND pr.as_of >= ?
			)
		), TRUE) AS complete
		FROM positions p
		JOIN accounts a ON a.id = p.account_id AND a.deleted_at IS NULL
		JOIN users    u ON u.id = p.user_id
		WHERE p.deleted_at IS NULL AND p.account_id IN ?
	`, freshAfter, accountIDs).Scan(&row).Error
	if err != nil {
		return decimal.Zero, false, err
	}
	d, _ := decimal.NewFromString(row.Balance)
	return d, row.Complete, nil
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
		  AND kind = 'flow'
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
		  AND t.kind = 'flow'
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
		  AND kind = 'flow'
		  AND transaction_date >= ?
		  AND transaction_date <  ?
	`, accountIDs, categoryID, from, to).Scan(&s).Error
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

func (r *householdAggregatorRepo) ListSharedBudgets(ctx context.Context, householdID int64, period string) ([]model.Budget, error) {
	q := r.db.WithContext(ctx).Where("household_id = ? AND is_active = ?", householdID, true)
	if period != "" {
		q = q.Where("period = ?", period)
	}
	var out []model.Budget
	if err := q.Order("id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *householdAggregatorRepo) ListSharedGoals(ctx context.Context, householdID int64) ([]model.SavingsGoal, error) {
	var out []model.SavingsGoal
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
// owner's primary currency. Used by AccountBalances (the per-account
// breakdown still values in each owner's currency). The net-worth trend and
// allocation rollups now value through the shared valuation derivation (#282)
// instead, so an unpriced asset surfaces as incomplete rather than $0.
// References: positions p, users u, prices pr.
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

func (r *householdAggregatorRepo) ListPositionsForAllocation(ctx context.Context, accountIDs []int64) ([]AllocationPosition, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	type rawRow struct {
		AssetID  int64
		Quantity string
		Kind     string
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT p.asset_id  AS asset_id,
		       p.quantity::text AS quantity,
		       ass.kind    AS kind
		FROM positions p
		JOIN accounts a ON a.id = p.account_id AND a.deleted_at IS NULL
		JOIN assets   ass ON ass.id = p.asset_id
		WHERE p.deleted_at IS NULL AND p.account_id IN ?
		ORDER BY ass.kind, p.asset_id
	`, accountIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]AllocationPosition, 0, len(rows))
	for _, row := range rows {
		q, err := decimal.NewFromString(row.Quantity)
		if err != nil {
			return nil, err
		}
		out = append(out, AllocationPosition{AssetID: row.AssetID, Quantity: q, Kind: row.Kind})
	}
	return out, nil
}

func (r *householdAggregatorRepo) FoldByAccountSetAsOf(ctx context.Context, accountIDs []int64, asOf time.Time) ([]model.Position, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	type row struct {
		AssetID int64
		Qty     string
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT asset_id, COALESCE(SUM(amount), 0)::text AS qty
		FROM transactions
		WHERE deleted_at IS NULL
		  AND account_id IN ?
		  AND transaction_date <= ?
		GROUP BY asset_id
		HAVING COALESCE(SUM(amount), 0) <> 0
		ORDER BY asset_id
	`, accountIDs, asOf).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]model.Position, 0, len(rows))
	for _, row := range rows {
		q, err := decimal.NewFromString(row.Qty)
		if err != nil {
			return nil, err
		}
		out = append(out, model.Position{AssetID: row.AssetID, Quantity: q})
	}
	return out, nil
}

func (r *householdAggregatorRepo) HouseholdQuoteAssetID(ctx context.Context, householdID int64) (int64, error) {
	var assetID int64
	err := r.db.WithContext(ctx).Raw(`
		SELECT u.primary_currency_asset_id
		FROM household_members hm
		JOIN users u ON u.id = hm.user_id
		WHERE hm.household_id = ?
		  AND hm.role = 'owner'
		  AND hm.purged_at IS NULL
		ORDER BY hm.joined_at
		LIMIT 1
	`, householdID).Scan(&assetID).Error
	if err != nil {
		return 0, err
	}
	if assetID == 0 {
		return 0, ErrNotFound
	}
	return assetID, nil
}

func (r *householdAggregatorRepo) AccountBalances(ctx context.Context, accountIDs []int64, freshAfter time.Time) ([]AccountBalanceRow, error) {
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
		Complete    bool
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT a.id           AS account_id,
		       a.name         AS name,
		       a.account_type AS account_type,
		       qa.symbol      AS currency,
		       a.user_id      AS owner_user_id,
		       COALESCE(SUM(`+fmt.Sprintf(positionValueExpr, "")+`), 0)::text AS balance,
		       COALESCE(BOOL_AND(
		           p.id IS NULL
		           OR p.asset_id = u.primary_currency_asset_id
		           OR EXISTS (
		               SELECT 1 FROM prices pr
		               WHERE pr.asset_id = p.asset_id
		                 AND pr.quote_asset_id = u.primary_currency_asset_id
		                 AND pr.as_of >= ?
		           )
		       ), TRUE) AS complete
		FROM accounts a
		JOIN users     u  ON u.id = a.user_id
		JOIN assets    qa ON qa.id = a.primary_quote_asset_id
		LEFT JOIN positions p ON p.deleted_at IS NULL AND p.account_id = a.id
		WHERE a.deleted_at IS NULL AND a.id IN ?
		GROUP BY a.id, a.name, a.account_type, qa.symbol, a.user_id
		ORDER BY a.id
	`, freshAfter, accountIDs).Scan(&rows).Error
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
			Complete:    row.Complete,
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

// CategoryTrend: outflows only, transfers excluded, grouped by (category,
// month) across the given account set, over the trailing `months` calendar
// months ending at the month containing `now`. Mirrors dashboardRepo's
// CategoryTrend, scoped by account set instead of user (#367).
func (r *householdAggregatorRepo) CategoryTrend(ctx context.Context, accountIDs []int64, now time.Time, months int) ([]CategoryMonthAmount, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	if months <= 0 {
		months = 6
	}
	now = now.UTC()
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	start := end.AddDate(0, -months, 0)

	type rawRow struct {
		CategoryID *int64
		Name       *string
		Month      time.Time
		Amount     string
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			t.category_id                                     AS category_id,
			c.name                                             AS name,
			date_trunc('month', t.transaction_date)::timestamptz AS month,
			COALESCE(SUM(-t.amount), 0)::text                  AS amount
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		WHERE t.deleted_at IS NULL
		  AND t.account_id IN ?
		  AND t.is_transfer = FALSE
		  AND t.kind = 'flow'
		  AND t.amount < 0
		  AND t.transaction_date >= ?
		  AND t.transaction_date <  ?
		GROUP BY t.category_id, c.name, date_trunc('month', t.transaction_date)
		ORDER BY month, name
	`, accountIDs, start, end).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]CategoryMonthAmount, 0, len(rows))
	for _, row := range rows {
		amt, _ := decimal.NewFromString(row.Amount)
		name := "Uncategorized"
		if row.Name != nil {
			name = *row.Name
		}
		out = append(out, CategoryMonthAmount{
			CategoryID: row.CategoryID,
			Name:       name,
			Month:      row.Month.UTC(),
			Amount:     amt,
		})
	}
	return out, nil
}

// TopMerchants mirrors dashboardRepo's TopMerchants, scoped by account set.
func (r *householdAggregatorRepo) TopMerchants(ctx context.Context, accountIDs []int64, from, to time.Time, limit int) ([]MerchantSpendItem, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	type rawRow struct {
		Merchant string
		Amount   string
		Count    int64
	}
	var rows []rawRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(NULLIF(t.merchant_name, ''), NULLIF(t.description_clean, ''), t.description, 'Unknown') AS merchant,
			COALESCE(SUM(-t.amount), 0)::text AS amount,
			COUNT(*)                          AS count
		FROM transactions t
		WHERE t.deleted_at IS NULL
		  AND t.account_id IN ?
		  AND t.is_transfer = FALSE
		  AND t.kind = 'flow'
		  AND t.amount < 0
		  AND t.transaction_date >= ?
		  AND t.transaction_date <  ?
		GROUP BY merchant
		ORDER BY SUM(-t.amount) DESC
		LIMIT ?
	`, accountIDs, from, to, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]MerchantSpendItem, 0, len(rows))
	for _, row := range rows {
		amt, _ := decimal.NewFromString(row.Amount)
		out = append(out, MerchantSpendItem{Merchant: row.Merchant, Amount: amt, Count: row.Count})
	}
	return out, nil
}

// CashFlowByMonth mirrors dashboardRepo's CashFlowByMonth, scoped by
// account set. Empty months (no activity) are zero-filled in Go so a quiet
// month doesn't drop out of the trend.
func (r *householdAggregatorRepo) CashFlowByMonth(ctx context.Context, accountIDs []int64, now time.Time, months int) ([]CashFlowMonth, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	if months <= 0 {
		months = 6
	}
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
			date_trunc('month', transaction_date)::timestamptz         AS month,
			COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0)::text   AS inflow,
			COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::text  AS outflow
		FROM transactions
		WHERE deleted_at IS NULL
		  AND account_id IN ?
		  AND is_transfer = FALSE
		  AND kind = 'flow'
		  AND transaction_date >= ?
		  AND transaction_date <  ?
		GROUP BY date_trunc('month', transaction_date)
	`, accountIDs, start, end).Scan(&rows).Error
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
