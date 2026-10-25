package household

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// Aggregator is the SINGLE cross-user reader for household surfaces. Per
// ADR-0008 and .claude/rules/go-backend.md, no other package may read across
// user_ids and this package must NEVER import pii_repo. The aggregator only
// returns aggregated values (sums, counts, percentages) and lightweight
// metadata — never raw transaction rows.
//
// Lifecycle filtering:
//   - LIVE aggregates (Dashboard, BudgetPace, GoalProgress current spend)
//     count only ACTIVE members (left_at IS NULL).
//   - HISTORICAL aggregates (returned alongside live ones where labeled)
//     include in-grace members (left_at IS NOT NULL AND now - left_at <= grace).
//   - PURGED members are excluded everywhere — they don't appear in
//     repository.ListMembersIncludingLeft to begin with.
type Aggregator struct {
	repo       repository.HouseholdAggregatorRepository
	households repository.HouseholdRepository
	val        *valuation.Service
	now        func() time.Time
}

func NewAggregator(
	repo repository.HouseholdAggregatorRepository,
	households repository.HouseholdRepository,
	val *valuation.Service,
) *Aggregator {
	return &Aggregator{
		repo:       repo,
		households: households,
		val:        val,
		now:        time.Now,
	}
}

// SetClock lets tests freeze time for grace-window assertions.
func (a *Aggregator) SetClock(fn func() time.Time) { a.now = fn }

// --- return types ---

// HouseholdDashboard mirrors the personal DashboardSummary but is keyed by
// household + aggregates only across opt-in shared accounts. All money fields
// are strings to preserve precision across the wire.
type HouseholdDashboard struct {
	Period   PeriodWindow `json:"period"`
	NetWorth string       `json:"net_worth"` // sum across balance_only + balance_and_txns shares
	// NetWorthComplete is false when a shared position had no price within
	// the valuation stale window — NetWorth may rest on stale rates (#344).
	NetWorthComplete bool `json:"net_worth_complete"`
	// NetWorthUnpriced names each asset left out of NetWorth (#601), so the
	// UI can say which price or exchange rate is missing; [] when complete.
	// Quote is the currency the owning member values it in. Catalog data
	// only — never which member or account holds it.
	NetWorthUnpriced []UnpricedAsset               `json:"net_worth_unpriced"`
	Income           string                        `json:"income"`            // period; balance_and_txns only
	Spending         string                        `json:"spending"`          // period; balance_and_txns only
	AccountCount     int                           `json:"account_count"`     // shared accounts count (any visibility)
	TransactionCount int64                         `json:"transaction_count"` // period; balance_and_txns only
	ByCategory       []CategorySpendingItem        `json:"by_category"`       // period; balance_and_txns only
	LiveMemberCount  int                           `json:"live_member_count"`
	InGraceCount     int                           `json:"in_grace_count"`
	Members          []HouseholdMemberContribution `json:"members"` // per-member breakdown for the same period
}

// UnpricedAsset is a held asset with no fresh price into Quote.
type UnpricedAsset struct {
	AssetID int64  `json:"asset_id"`
	Symbol  string `json:"symbol"`
	Kind    string `json:"kind"`
	Quote   string `json:"quote"`
	// QuoteAssetID lets Set rate write the pair this headline reads, even
	// when the member setting it has a different primary currency.
	QuoteAssetID int64 `json:"quote_asset_id"`
}

// HouseholdMemberContribution is one row of the per-member tile strip on
// /h/dashboard. Each value is scoped to that member's shared accounts —
// `private` accounts contribute nothing here, and `balance_only` accounts
// only feed `NetWorthContribution` (their txns aren't visible).
//
// Privacy: this is still aggregator output — no raw transactions, no
// account names, just one row per active member with their own user_id
// (which the requester is already a peer of in the household scope, so
// it is not PII at this layer).
type HouseholdMemberContribution struct {
	UserID               int64  `json:"user_id"`
	Role                 string `json:"role"`
	NetWorthContribution string `json:"net_worth_contribution"`
	SpendingContribution string `json:"spending_contribution"`
	AccountCount         int    `json:"account_count"`
}

// PeriodWindow is the resolved [from, to) window. Mirrors service.PeriodWindow
// in shape; we keep it local so service/household doesn't import service/.
type PeriodWindow struct {
	Key  string    `json:"key"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type CategorySpendingItem struct {
	CategoryID *int64 `json:"category_id"`
	Name       string `json:"name"`
	Amount     string `json:"amount"`
}

// BudgetPaceItem is one shared_budget rolled up with its current-period spend.
// Pace is spend / budget (when budget > 0); 0 otherwise.
type BudgetPaceItem struct {
	BudgetID   int64  `json:"budget_id"`
	CategoryID int64  `json:"category_id"`
	Period     string `json:"period"`
	Budget     string `json:"budget"`
	Spent      string `json:"spent"`
	// Carryover is the unspent balance rolled in from earlier months (zero
	// unless the budget has rollover on and the window is current_month).
	// Available = Budget + Carryover; Pace is Spent / Available.
	Carryover string `json:"carryover"`
	Available string `json:"available"`
	Remaining string `json:"remaining"` // Available - Spent (negative when over)
	Pace      string `json:"pace"`      // ratio 0..N (1.0 = on budget)
}

// BudgetSummary is the household's current-month budget totals (#589):
// budgeted, spent against budgets, and spent outside any budget. Figures
// are sums over balance_and_txns accounts of live members only — the same
// accounts BudgetPace and the Dashboard spending figure use — so
// SpentInBudgets + NotInBudget == Dashboard.Spending by construction. No
// raw transaction rows; Unbudgeted is a per-category sum.
type BudgetSummary struct {
	Month          string            `json:"month"` // YYYY-MM
	PeriodStart    time.Time         `json:"period_start"`
	PeriodEnd      time.Time         `json:"period_end"` // exclusive
	BudgetCount    int               `json:"budget_count"`
	Budgeted       string            `json:"budgeted"`
	Carryover      string            `json:"carryover"`
	Available      string            `json:"available"`
	SpentInBudgets string            `json:"spent_in_budgets"`
	Remaining      string            `json:"remaining"`
	NotInBudget    string            `json:"not_in_budget"`
	TotalSpending  string            `json:"total_spending"`
	Unbudgeted     []UnbudgetedSpend `json:"unbudgeted"`
}

// UnbudgetedSpend is one category's spending with no active shared budget
// (CategoryID nil = uncategorized). An aggregate, never a transaction.
type UnbudgetedSpend struct {
	CategoryID *int64 `json:"category_id"`
	Spent      string `json:"spent"`
}

// GoalProgressItem reports completion of a shared_goal.
type GoalProgressItem struct {
	GoalID        int64      `json:"goal_id"`
	Name          string     `json:"name"`
	TargetAmount  string     `json:"target_amount"`
	CurrentAmount string     `json:"current_amount"`
	Progress      string     `json:"progress"` // ratio 0..1
	TargetDate    *time.Time `json:"target_date,omitempty"`
}

// HouseholdAIContext is what the AI advisor sees when the requester is in a
// household. Includes the LIVE dashboard summary, the shared-thread list, AND
// the requester's PERSONAL threads — never another member's private threads.
type HouseholdAIContext struct {
	HouseholdID     int64           `json:"household_id"`
	NetWorth        string          `json:"net_worth"`
	Income          string          `json:"income"`
	Spending        string          `json:"spending"`
	Period          PeriodWindow    `json:"period"`
	SharedThreads   []ThreadSummary `json:"shared_threads"`
	PersonalThreads []ThreadSummary `json:"personal_threads"`
}

// ThreadSummary excludes message content — the aggregator never returns raw
// chat bodies; the AI service hydrates messages on demand under tenant rules.
type ThreadSummary struct {
	ID                  int64     `json:"id"`
	UserID              int64     `json:"user_id"`
	Title               *string   `json:"title,omitempty"`
	SharedWithHousehold bool      `json:"shared_with_household"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// AssetClassAllocation is one row of the household allocation rollup — the
// total value of shared positions whose asset kind matches, in the household
// quote currency (the owning member's primary currency). The wire string keeps
// NUMERIC precision intact. Complete is false when at least one position of
// this kind had no available price, so Value is a partial sum (#282) — the UI
// can flag the bucket as incomplete rather than understate it silently.
type AssetClassAllocation struct {
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Complete bool   `json:"complete"`
}

// NetWorthPoint is one (date, value) row of the household net-worth trend.
// Date is the month-end instant; Value is the rolled-up net worth across
// shared accounts owned by live members at that point, in the household quote
// currency. Complete is false when an asset held at that month-end had no
// available price — Value is then a partial sum (#282), not a silent $0.
type NetWorthPoint struct {
	Date     time.Time `json:"date"`
	Value    string    `json:"value"`
	Complete bool      `json:"complete"`
	// ByKind splits Value by asset kind (#374 drill-in), sorted by kind. The
	// slices sum to Value; a kind with an unpriced position is Complete=false.
	ByKind []AssetClassAllocation `json:"by_kind,omitempty"`
}

// AccountSummary is one row of the household account breakdown. Aggregator
// output: lightweight projection (name, type, balance, owner, visibility)
// — never a raw account row, never PII. The OwnerUserID is the household
// peer's user_id (already known to other members in /h/members) and is not
// treated as PII at this layer.
type AccountSummary struct {
	AccountID   int64  `json:"account_id"`
	Name        string `json:"name"`
	AccountType string `json:"account_type"`
	Currency    string `json:"currency"`
	Balance     string `json:"balance"`
	OwnerUserID int64  `json:"owner_user_id"`
	Visibility  string `json:"visibility"`
	// Complete is false when a position in this account had no price
	// observed within the valuation stale window — Balance may rest on a
	// stale rate, so the UI flags it as partial (#339, #282 contract).
	Complete bool `json:"complete"`
}

// --- core methods ---

// Dashboard returns the household-level rollup for the period. Aggregates over
// shared accounts owned by ACTIVE members (live). In-grace members are not
// included in live spend — but their member count is surfaced separately so
// the UI can hint at the lifecycle state.
func (a *Aggregator) Dashboard(ctx context.Context, householdID int64, period string) (*HouseholdDashboard, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	from, to, err := ResolvePeriod(period, a.now())
	if err != nil {
		return nil, err
	}

	live, inGrace, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)

	netWorthShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceOnly, model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares (net-worth): %w", err)
	}
	netWorthAccounts := filterAccountsByUsers(netWorthShares, liveUserIDs)

	txShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares (txns): %w", err)
	}
	txAccounts := filterAccountsByUsers(txShares, liveUserIDs)

	freshAfter := a.now().Add(-a.val.StaleWindow())
	netWorth, netWorthComplete, err := a.repo.SumBalances(ctx, netWorthAccounts, freshAfter)
	if err != nil {
		return nil, fmt.Errorf("sum balances: %w", err)
	}
	unpriced := []UnpricedAsset{}
	if !netWorthComplete {
		rows, err := a.repo.UnpricedAssets(ctx, netWorthAccounts, freshAfter)
		if err != nil {
			return nil, fmt.Errorf("unpriced assets: %w", err)
		}
		for _, r := range rows {
			unpriced = append(unpriced, UnpricedAsset{AssetID: r.AssetID, Symbol: r.Symbol, Kind: r.Kind, Quote: r.Quote, QuoteAssetID: r.QuoteAssetID})
		}
	}
	income, spending, err := a.repo.PeriodIncomeSpending(ctx, txAccounts, from, to)
	if err != nil {
		return nil, fmt.Errorf("income/spending: %w", err)
	}
	txCount, err := a.repo.PeriodTransactionCount(ctx, txAccounts, from, to)
	if err != nil {
		return nil, fmt.Errorf("transaction count: %w", err)
	}
	rawCats, err := a.repo.CategoryAggregates(ctx, txAccounts, from, to)
	if err != nil {
		return nil, fmt.Errorf("category aggregates: %w", err)
	}
	cats := make([]CategorySpendingItem, 0, len(rawCats))
	for _, c := range rawCats {
		cats = append(cats, CategorySpendingItem{
			CategoryID: c.CategoryID,
			Name:       c.Name,
			Amount:     c.Amount.String(),
		})
	}

	members, err := a.perMemberContributions(ctx, live, netWorthShares, txShares, from, to)
	if err != nil {
		return nil, fmt.Errorf("per-member: %w", err)
	}

	return &HouseholdDashboard{
		Period:           PeriodWindow{Key: period, From: from, To: to},
		NetWorth:         netWorth.String(),
		NetWorthComplete: netWorthComplete,
		NetWorthUnpriced: unpriced,
		Income:           income.String(),
		Spending:         spending.String(),
		AccountCount:     len(netWorthAccounts),
		TransactionCount: txCount,
		ByCategory:       cats,
		LiveMemberCount:  len(live),
		InGraceCount:     len(inGrace),
		Members:          members,
	}, nil
}

// perMemberContributions partitions the household's shared accounts by
// owning user_id and computes each active member's contribution to net
// worth (balance_only ∪ balance_and_txns) and period spending
// (balance_and_txns only). The result is ordered to match `live`'s
// canonical role-then-id sort.
//
// Members with no shared accounts at all are omitted — the UI's tile
// strip rendering each member regardless can fall back to the role chip
// from /h/members. This keeps the dashboard endpoint focused on aggregate
// contributions.
func (a *Aggregator) perMemberContributions(
	ctx context.Context,
	live []model.HouseholdMember,
	netWorthShares, txShares []repository.AccountShareView,
	from, to time.Time,
) ([]HouseholdMemberContribution, error) {
	// Bucket account IDs by user.
	netWorthByUser := map[int64][]int64{}
	for _, s := range netWorthShares {
		netWorthByUser[s.UserID] = append(netWorthByUser[s.UserID], s.AccountID)
	}
	txByUser := map[int64][]int64{}
	for _, s := range txShares {
		txByUser[s.UserID] = append(txByUser[s.UserID], s.AccountID)
	}

	out := make([]HouseholdMemberContribution, 0, len(live))
	for _, m := range live {
		nwIDs := netWorthByUser[m.UserID]
		txIDs := txByUser[m.UserID]
		if len(nwIDs) == 0 && len(txIDs) == 0 {
			continue
		}
		// Per-member completeness isn't surfaced on the tile strip yet —
		// the headline flag (above) covers the same account set.
		nw, _, err := a.repo.SumBalances(ctx, nwIDs, a.now().Add(-a.val.StaleWindow()))
		if err != nil {
			return nil, fmt.Errorf("sum balances for user %d: %w", m.UserID, err)
		}
		_, spend, err := a.repo.PeriodIncomeSpending(ctx, txIDs, from, to)
		if err != nil {
			return nil, fmt.Errorf("period spend for user %d: %w", m.UserID, err)
		}
		out = append(out, HouseholdMemberContribution{
			UserID:               m.UserID,
			Role:                 m.Role,
			NetWorthContribution: nw.String(),
			SpendingContribution: spend.String(),
			AccountCount:         len(nwIDs),
		})
	}
	return out, nil
}

// BudgetPace rolls up each shared_budget for the household against its
// current-period spend (across balance_and_txns shares owned by live members).
func (a *Aggregator) BudgetPace(ctx context.Context, householdID int64, period string) ([]BudgetPaceItem, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	from, to, err := ResolvePeriod(period, a.now())
	if err != nil {
		return nil, err
	}

	budgets, err := a.repo.ListSharedBudgets(ctx, householdID, "")
	if err != nil {
		return nil, fmt.Errorf("list household budgets: %w", err)
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	txShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	txAccounts := filterAccountsByUsers(txShares, liveUserIDs)

	out := make([]BudgetPaceItem, 0, len(budgets))
	for _, b := range budgets {
		spent, err := a.repo.SpendingByCategory(ctx, txAccounts, b.CategoryID, from, to)
		if err != nil {
			return nil, fmt.Errorf("spending for budget %d: %w", b.ID, err)
		}
		carry := decimal.Zero
		if b.Rollover && period == PeriodCurrentMonth {
			carry, err = a.monthlyCarryover(ctx, txAccounts, &b, from)
			if err != nil {
				return nil, err
			}
		}
		available := b.Amount.Add(carry)
		pace := decimal.Zero
		if !available.IsZero() {
			pace = spent.Div(available)
		}
		out = append(out, BudgetPaceItem{
			BudgetID:   b.ID,
			CategoryID: b.CategoryID,
			Period:     b.Period,
			Budget:     b.Amount.String(),
			Spent:      spent.String(),
			Carryover:  carry.String(),
			Available:  available.String(),
			Remaining:  available.Sub(spent).String(),
			Pace:       pace.String(),
		})
	}
	return out, nil
}

// BudgetSummary returns the current month's shared-budget totals (#589).
// Shared budgets are tracked per calendar month, so every active budget
// counts, with the same spend and carryover as its BudgetPace card.
func (a *Aggregator) BudgetSummary(ctx context.Context, householdID int64) (*BudgetSummary, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	from, to, err := ResolvePeriod(PeriodCurrentMonth, a.now())
	if err != nil {
		return nil, err
	}
	budgets, err := a.repo.ListSharedBudgets(ctx, householdID, "")
	if err != nil {
		return nil, fmt.Errorf("list household budgets: %w", err)
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	txShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	txAccounts := filterAccountsByUsers(txShares, userIDs(live))

	var count int
	budgeted, carryover, spentIn := decimal.Zero, decimal.Zero, decimal.Zero
	inBudget := map[int64]bool{}
	for i := range budgets {
		b := &budgets[i]
		if !b.IsActive || inBudget[b.CategoryID] {
			continue
		}
		spent, err := a.repo.SpendingByCategory(ctx, txAccounts, b.CategoryID, from, to)
		if err != nil {
			return nil, fmt.Errorf("spending for budget %d: %w", b.ID, err)
		}
		carry := decimal.Zero
		if b.Rollover {
			if carry, err = a.monthlyCarryover(ctx, txAccounts, b, from); err != nil {
				return nil, err
			}
		}
		count++
		budgeted = budgeted.Add(b.Amount)
		carryover = carryover.Add(carry)
		spentIn = spentIn.Add(spent)
		inBudget[b.CategoryID] = true
	}

	cats, err := a.repo.SpendingAllCategories(ctx, txAccounts, from, to)
	if err != nil {
		return nil, fmt.Errorf("month spending: %w", err)
	}
	total, notIn := decimal.Zero, decimal.Zero
	unbudgeted := make([]UnbudgetedSpend, 0, len(cats))
	for _, c := range cats {
		total = total.Add(c.Spent)
		if c.CategoryID != nil && inBudget[*c.CategoryID] {
			continue
		}
		notIn = notIn.Add(c.Spent)
		unbudgeted = append(unbudgeted, UnbudgetedSpend{CategoryID: c.CategoryID, Spent: c.Spent.String()})
	}
	available := budgeted.Add(carryover)
	return &BudgetSummary{
		Month:          from.Format("2006-01"),
		PeriodStart:    from,
		PeriodEnd:      to,
		BudgetCount:    count,
		Budgeted:       budgeted.String(),
		Carryover:      carryover.String(),
		Available:      available.String(),
		SpentInBudgets: spentIn.String(),
		Remaining:      available.Sub(spentIn).String(),
		NotInBudget:    notIn.String(),
		TotalSpending:  total.String(),
		Unbudgeted:     unbudgeted,
	}, nil
}

// maxRolloverMonths mirrors the personal-budget cap (BudgetService).
const maxRolloverMonths = 36

// monthlyCarryover applies the personal rollover policy to a shared budget:
// the balance starts at b.RolloverStart (default zero) in the month the
// budget was created, each
// completed month adds (amount - spent), and the balance never drops below
// zero. Spend is read through the same share/visibility filter as the
// current month, so it reflects today's shares, not historical ones.
func (a *Aggregator) monthlyCarryover(ctx context.Context, accountIDs []int64, b *model.Budget, currentFrom time.Time) (decimal.Decimal, error) {
	created := b.CreatedAt.UTC()
	start := time.Date(created.Year(), created.Month(), 1, 0, 0, 0, 0, time.UTC)
	cur := time.Date(currentFrom.Year(), currentFrom.Month(), 1, 0, 0, 0, 0, time.UTC)
	earliest := cur.AddDate(0, -maxRolloverMonths, 0)
	carry := decimal.Zero
	if !earliest.After(start) {
		earliest = start
		carry = b.RolloverStart
	}
	if !earliest.Before(cur) {
		return carry, nil
	}
	spend, err := a.repo.SpendingByCategoryMonthly(ctx, accountIDs, b.CategoryID, earliest, cur)
	if err != nil {
		return decimal.Zero, fmt.Errorf("rollover for budget %d: %w", b.ID, err)
	}
	for m := earliest; m.Before(cur); m = m.AddDate(0, 1, 0) {
		carry = decimal.Max(decimal.Zero, carry.Add(b.Amount).Sub(spend[m]))
	}
	return carry, nil
}

// GoalProgress returns each shared_goal's progress ratio. No transaction
// queries — current_amount is canonical state.
func (a *Aggregator) GoalProgress(ctx context.Context, householdID int64) ([]GoalProgressItem, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	goals, err := a.repo.ListSharedGoals(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list household goals: %w", err)
	}
	out := make([]GoalProgressItem, 0, len(goals))
	for _, g := range goals {
		progress := decimal.Zero
		if !g.TargetAmount.IsZero() {
			progress = g.CurrentAmount.Div(g.TargetAmount)
		}
		out = append(out, GoalProgressItem{
			GoalID:        g.ID,
			Name:          g.Name,
			TargetAmount:  g.TargetAmount.String(),
			CurrentAmount: g.CurrentAmount.String(),
			Progress:      progress.String(),
			TargetDate:    g.TargetDate,
		})
	}
	return out, nil
}

// AIContext bundles the live dashboard summary, the household's shared
// threads, and the REQUESTER's personal threads. It NEVER returns another
// member's personal threads.
func (a *Aggregator) AIContext(ctx context.Context, householdID, requesterUserID int64) (*HouseholdAIContext, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	from, to, err := ResolvePeriod(PeriodCurrentMonth, a.now())
	if err != nil {
		return nil, err
	}

	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	nwShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceOnly, model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, err
	}
	nwAccounts := filterAccountsByUsers(nwShares, liveUserIDs)
	txShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, err
	}
	txAccounts := filterAccountsByUsers(txShares, liveUserIDs)

	// Completeness is not part of the AI context — the model gets the
	// partial sum either way; flagging is a UI concern.
	netWorth, _, err := a.repo.SumBalances(ctx, nwAccounts, a.now().Add(-a.val.StaleWindow()))
	if err != nil {
		return nil, err
	}
	income, spending, err := a.repo.PeriodIncomeSpending(ctx, txAccounts, from, to)
	if err != nil {
		return nil, err
	}

	shared, err := a.repo.ListSharedThreads(ctx, householdID, 50)
	if err != nil {
		return nil, err
	}
	personal, err := a.repo.ListPersonalThreadsForUser(ctx, requesterUserID, 50)
	if err != nil {
		return nil, err
	}

	return &HouseholdAIContext{
		HouseholdID:     householdID,
		NetWorth:        netWorth.String(),
		Income:          income.String(),
		Spending:        spending.String(),
		Period:          PeriodWindow{Key: PeriodCurrentMonth, From: from, To: to},
		SharedThreads:   toThreadSummaries(shared),
		PersonalThreads: toThreadSummaries(personal),
	}, nil
}

// Allocation returns the household's positions rolled up by asset kind
// (fiat / equity / fund / crypto / …) across shared accounts owned by
// LIVE members. Visibility floor is balance_only — private accounts
// have no share row to begin with, so they're excluded by construction.
// In-grace members are excluded from this LIVE rollup.
func (a *Aggregator) Allocation(ctx context.Context, householdID int64) ([]AssetClassAllocation, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	shares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceOnly, model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	accountIDs := filterAccountsByUsers(shares, liveUserIDs)

	quoteAssetID, err := a.repo.HouseholdQuoteAssetID(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("household quote currency: %w", err)
	}
	positions, err := a.repo.ListPositionsForAllocation(ctx, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("list positions for allocation: %w", err)
	}

	// Value each position through the shared derivation (current snapshot) and
	// bucket by asset kind. An unpriced position marks its kind incomplete
	// instead of contributing a silent $0 (#282).
	now := a.now().UTC()
	type bucket struct {
		value    decimal.Decimal
		complete bool
		order    int
	}
	buckets := map[string]*bucket{}
	order := 0
	ensure := func(kind string) *bucket {
		b, ok := buckets[kind]
		if !ok {
			b = &bucket{value: decimal.Zero, complete: true, order: order}
			order++
			buckets[kind] = b
		}
		return b
	}
	for _, p := range positions {
		b := ensure(p.Kind)
		v, err := a.val.Value(ctx, model.Position{AssetID: p.AssetID, Quantity: p.Quantity}, now, quoteAssetID)
		if errors.Is(err, valuation.ErrStalePrice) {
			b.complete = false
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("value position (asset=%d): %w", p.AssetID, err)
		}
		b.value = b.value.Add(v)
	}

	out := make([]AssetClassAllocation, len(buckets))
	for kind, b := range buckets {
		out[b.order] = AssetClassAllocation{Kind: kind, Value: b.value.String(), Complete: b.complete}
	}
	return out, nil
}

// NetWorthTrend returns a month-end net-worth series for the trailing `months`
// calendar months ending at the aggregator's clock, via the unified valuation
// derivation (#282) — identical to the personal trend for the same account set
// + quote currency. At each month-end, quantity per asset is the transaction
// fold across the shared account set up to that date (ADR-0017), priced at
// that date in the household quote currency. A month-end with an unpriced asset
// is reported incomplete rather than silently understated.
//
// Defaults: months ≤ 0 → 12. Excludes private accounts and in-grace members
// — same lifecycle rules as Dashboard/Allocation.
func (a *Aggregator) NetWorthTrend(ctx context.Context, householdID int64, months int) ([]NetWorthPoint, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	if months <= 0 {
		months = 12
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	shares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceOnly, model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	accountIDs := filterAccountsByUsers(shares, liveUserIDs)

	quoteAssetID, err := a.repo.HouseholdQuoteAssetID(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("household quote currency: %w", err)
	}

	// Same derivation + grid as the personal trend (valuation.Series over a
	// month-end grid), differing only in the account set (shared accounts of
	// live members) and quote currency (#282). positionsAt folds transactions
	// across the shared set up to each month-end — historical quantity from
	// the ledger, not today's positions.
	asOfs := valuation.MonthEndGrid(a.now(), months)
	val := a.val.WithStaleWindow(0)
	kindOf := map[int64]string{}
	byKind := map[time.Time][]AssetClassAllocation{}
	series, err := val.Series(ctx, asOfs, quoteAssetID,
		func(ctx context.Context, asOf time.Time) ([]model.Position, error) {
			positions, err := a.repo.FoldByAccountSetAsOf(ctx, accountIDs, asOf)
			if err != nil {
				return nil, err
			}
			var missing []int64
			for _, p := range positions {
				if _, ok := kindOf[p.AssetID]; !ok {
					missing = append(missing, p.AssetID)
				}
			}
			if len(missing) > 0 {
				kinds, err := a.repo.AssetKinds(ctx, missing)
				if err != nil {
					return nil, fmt.Errorf("asset kinds: %w", err)
				}
				for id, k := range kinds {
					kindOf[id] = k
				}
			}
			groups := map[string][]model.Position{}
			for _, p := range positions {
				groups[kindOf[p.AssetID]] = append(groups[kindOf[p.AssetID]], p)
			}
			kinds := make([]string, 0, len(groups))
			for k := range groups {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			for _, k := range kinds {
				v, err := val.ValuePositions(ctx, groups[k], asOf, quoteAssetID)
				if err != nil {
					return nil, fmt.Errorf("value kind %s: %w", k, err)
				}
				byKind[asOf] = append(byKind[asOf], AssetClassAllocation{Kind: k, Value: v.Value.String(), Complete: v.Complete()})
			}
			return positions, nil
		})
	if err != nil {
		return nil, fmt.Errorf("net worth series: %w", err)
	}
	out := make([]NetWorthPoint, 0, len(series))
	for _, p := range series {
		out = append(out, NetWorthPoint{
			Date:     p.AsOf,
			Value:    p.Value.String(),
			Complete: p.Complete(),
			ByKind:   byKind[p.AsOf],
		})
	}
	return out, nil
}

// AccountSummaries returns one row per SHARED account, including
// balance_only and balance_and_txns (private accounts have no share row).
// Balance is computed in the OWNER's primary currency. Visibility comes
// from the share row so the UI can render the chip without an extra
// fetch. Owned by an in-grace member ⇒ excluded from live output.
func (a *Aggregator) AccountSummaries(ctx context.Context, householdID int64) ([]AccountSummary, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	shares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceOnly, model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	type shareMeta struct {
		visibility string
	}
	meta := make(map[int64]shareMeta, len(shares))
	accountIDs := make([]int64, 0, len(shares))
	for _, s := range shares {
		if _, ok := liveUserIDs[s.UserID]; !ok {
			continue
		}
		meta[s.AccountID] = shareMeta{visibility: s.Visibility}
		accountIDs = append(accountIDs, s.AccountID)
	}
	balances, err := a.repo.AccountBalances(ctx, accountIDs, a.now().Add(-a.val.StaleWindow()))
	if err != nil {
		return nil, fmt.Errorf("account balances: %w", err)
	}
	out := make([]AccountSummary, 0, len(balances))
	for _, b := range balances {
		out = append(out, AccountSummary{
			AccountID:   b.AccountID,
			Name:        b.Name,
			AccountType: b.AccountType,
			Currency:    b.Currency,
			Balance:     b.Balance.String(),
			OwnerUserID: b.OwnerUserID,
			Visibility:  meta[b.AccountID].visibility,
			Complete:    b.Complete,
		})
	}
	return out, nil
}

// CategoryTrendMonth is one month of a category's spend series (#367).
// Amount is a positive decimal string (outflow sign flipped).
type CategoryTrendMonth struct {
	Month  string `json:"month"` // YYYY-MM-DD (first of month)
	Amount string `json:"amount"`
}

// CategoryTrendItem is one category's month-over-month spend series across
// shared accounts, plus the this-month vs. trailing-average comparison
// (#367) — the household analogue of service.DashboardService.CategoryTrend.
// Wire-identical shape so the Insights hook renders both scopes uniformly.
type CategoryTrendItem struct {
	CategoryID      *int64               `json:"category_id"`
	Name            string               `json:"name"`
	Months          []CategoryTrendMonth `json:"months"`
	ThisMonth       string               `json:"this_month"`
	TrailingAverage string               `json:"trailing_average"`
}

// MerchantSpendItem is one row of the household top-merchants view (#367).
// Amount is a positive decimal string (outflow sign flipped). Sourced only
// from balance_and_txns shares — a merchant name is transaction-level detail
// the same visibility floor already gates.
type MerchantSpendItem struct {
	Merchant string `json:"merchant"`
	Amount   string `json:"amount"`
	Count    int64  `json:"count"`
}

// CashFlowMonth is one month of the household income-vs-spending trend
// (#367). Inflow/Outflow are positive; Net = Inflow - Outflow.
type CashFlowMonth struct {
	Month   string `json:"month"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

// CategoryTrend returns the month-over-month spending trend per category
// across the household's balance_and_txns shares owned by LIVE members
// (#367) — same visibility floor as BudgetPace/CategoryAggregates. Defaults
// to 6 months.
func (a *Aggregator) CategoryTrend(ctx context.Context, householdID int64, months int) ([]CategoryTrendItem, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	if months <= 0 {
		months = 6
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	txShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	txAccounts := filterAccountsByUsers(txShares, liveUserIDs)

	now := a.now().UTC()
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	start := end.AddDate(0, -months, 0)

	rows, err := a.repo.CategoryTrend(ctx, txAccounts, now, months)
	if err != nil {
		return nil, fmt.Errorf("category trend: %w", err)
	}

	// catKey groups by category value, not pointer identity — each SQL row
	// scan allocates a fresh *int64 even for the same category id, so using
	// r.CategoryID directly as (part of) a map key would split one category
	// into as many buckets as it has rows across the window.
	type catKey struct {
		hasID bool
		id    int64
		name  string
	}
	byCat := map[catKey]map[time.Time]decimal.Decimal{}
	var order []catKey
	for _, r := range rows {
		k := catKey{name: r.Name}
		if r.CategoryID != nil {
			k.hasID = true
			k.id = *r.CategoryID
		}
		if _, ok := byCat[k]; !ok {
			byCat[k] = map[time.Time]decimal.Decimal{}
			order = append(order, k)
		}
		byCat[k][r.Month.UTC()] = r.Amount
	}

	items := make([]CategoryTrendItem, 0, len(order))
	for _, k := range order {
		monthAmts := byCat[k]
		monthsOut := make([]CategoryTrendMonth, 0, months)
		trailingSum := decimal.Zero
		trailingCount := 0
		thisMonth := decimal.Zero
		for i := 0; i < months; i++ {
			m := start.AddDate(0, i, 0)
			amt, ok := monthAmts[m]
			if !ok {
				amt = decimal.Zero
			}
			monthsOut = append(monthsOut, CategoryTrendMonth{Month: m.Format("2006-01-02"), Amount: amt.String()})
			if i == months-1 {
				thisMonth = amt
			} else {
				trailingSum = trailingSum.Add(amt)
				trailingCount++
			}
		}
		trailingAvg := decimal.Zero
		if trailingCount > 0 {
			trailingAvg = trailingSum.Div(decimal.NewFromInt(int64(trailingCount)))
		}
		var categoryID *int64
		if k.hasID {
			id := k.id
			categoryID = &id
		}
		items = append(items, CategoryTrendItem{
			CategoryID:      categoryID,
			Name:            k.name,
			Months:          monthsOut,
			ThisMonth:       thisMonth.String(),
			TrailingAverage: trailingAvg.String(),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		vi, _ := decimal.NewFromString(items[i].ThisMonth)
		vj, _ := decimal.NewFromString(items[j].ThisMonth)
		return vi.GreaterThan(vj)
	})
	return items, nil
}

// TopMerchants returns the top-spending merchants over [from, to) across
// the household's balance_and_txns shares owned by LIVE members (#367).
func (a *Aggregator) TopMerchants(ctx context.Context, householdID int64, from, to time.Time, limit int) ([]MerchantSpendItem, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	if from.IsZero() || to.IsZero() {
		f, t, err := ResolvePeriod(PeriodCurrentMonth, a.now())
		if err != nil {
			return nil, err
		}
		from, to = f, t
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	txShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	txAccounts := filterAccountsByUsers(txShares, liveUserIDs)

	rows, err := a.repo.TopMerchants(ctx, txAccounts, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("top merchants: %w", err)
	}
	out := make([]MerchantSpendItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, MerchantSpendItem{Merchant: r.Merchant, Amount: r.Amount.String(), Count: r.Count})
	}
	return out, nil
}

// CashFlow returns the trailing `months` months of income vs. spending
// across the household's balance_and_txns shares owned by LIVE members
// (#367) — the household analogue of service.DashboardService.CashFlow.
// Defaults to 6 months.
func (a *Aggregator) CashFlow(ctx context.Context, householdID int64, months int) ([]CashFlowMonth, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	if months <= 0 {
		months = 6
	}
	live, _, err := a.liveAndInGrace(ctx, householdID)
	if err != nil {
		return nil, err
	}
	liveUserIDs := userIDs(live)
	txShares, err := a.repo.ListAccountShares(ctx, householdID,
		[]string{model.VisibilityBalanceAndTxns})
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	txAccounts := filterAccountsByUsers(txShares, liveUserIDs)

	rows, err := a.repo.CashFlowByMonth(ctx, txAccounts, a.now(), months)
	if err != nil {
		return nil, fmt.Errorf("cash flow: %w", err)
	}
	out := make([]CashFlowMonth, 0, len(rows))
	for _, r := range rows {
		out = append(out, CashFlowMonth{
			Month:   r.Month.Format("2006-01-02"),
			Inflow:  r.Inflow.String(),
			Outflow: r.Outflow.String(),
			Net:     r.Net.String(),
		})
	}
	return out, nil
}

// --- internal helpers ---

func (a *Aggregator) requireHousehold(ctx context.Context, householdID int64) error {
	if _, err := a.households.GetByID(ctx, householdID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrHouseholdNotFound
		}
		return err
	}
	return nil
}

// liveAndInGrace partitions members into active and in-grace lists, using
// the household's grace_period_days as the cutoff for "in-grace". Purged
// members are absent from ListMembersIncludingLeft.
func (a *Aggregator) liveAndInGrace(ctx context.Context, householdID int64) ([]model.HouseholdMember, []model.HouseholdMember, error) {
	hh, err := a.households.GetByID(ctx, householdID)
	if err != nil {
		return nil, nil, err
	}
	all, err := a.repo.ListMembersIncludingLeft(ctx, householdID)
	if err != nil {
		return nil, nil, fmt.Errorf("list members: %w", err)
	}
	now := a.now()
	grace := time.Duration(hh.GracePeriodDays) * 24 * time.Hour
	var live, inGrace []model.HouseholdMember
	for _, m := range all {
		if m.LeftAt == nil {
			live = append(live, m)
			continue
		}
		if now.Sub(*m.LeftAt) <= grace {
			inGrace = append(inGrace, m)
		}
		// Beyond grace: dropped from BOTH lists. The purge runner (deferred)
		// will set purged_at; until then the row is invisible to live readers.
	}
	return live, inGrace, nil
}

func userIDs(members []model.HouseholdMember) map[int64]struct{} {
	out := make(map[int64]struct{}, len(members))
	for _, m := range members {
		out[m.UserID] = struct{}{}
	}
	return out
}

func filterAccountsByUsers(shares []repository.AccountShareView, allowedUsers map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(shares))
	for _, s := range shares {
		if _, ok := allowedUsers[s.UserID]; ok {
			out = append(out, s.AccountID)
		}
	}
	return out
}

func toThreadSummaries(threads []model.AIThread) []ThreadSummary {
	out := make([]ThreadSummary, 0, len(threads))
	for _, t := range threads {
		out = append(out, ThreadSummary{
			ID:                  t.ID,
			UserID:              t.UserID,
			Title:               t.Title,
			SharedWithHousehold: t.SharedWithHousehold,
			UpdatedAt:           t.UpdatedAt,
		})
	}
	return out
}
