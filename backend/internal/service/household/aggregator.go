package household

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
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
	repo            repository.HouseholdAggregatorRepository
	households      repository.HouseholdRepository
	now             func() time.Time
}

func NewAggregator(
	repo repository.HouseholdAggregatorRepository,
	households repository.HouseholdRepository,
) *Aggregator {
	return &Aggregator{
		repo:       repo,
		households: households,
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
	Period           PeriodWindow              `json:"period"`
	NetWorth         string                    `json:"net_worth"`         // sum across balance_only + balance_and_txns shares
	Income           string                    `json:"income"`            // period; balance_and_txns only
	Spending         string                    `json:"spending"`          // period; balance_and_txns only
	AccountCount     int                       `json:"account_count"`     // shared accounts count (any visibility)
	TransactionCount int64                     `json:"transaction_count"` // period; balance_and_txns only
	ByCategory       []CategorySpendingItem    `json:"by_category"`       // period; balance_and_txns only
	LiveMemberCount  int                       `json:"live_member_count"`
	InGraceCount     int                       `json:"in_grace_count"`
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
	Pace       string `json:"pace"` // ratio 0..N (1.0 = on budget)
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
	HouseholdID      int64                `json:"household_id"`
	NetWorth         string               `json:"net_worth"`
	Income           string               `json:"income"`
	Spending         string               `json:"spending"`
	Period           PeriodWindow         `json:"period"`
	SharedThreads    []ThreadSummary      `json:"shared_threads"`
	PersonalThreads  []ThreadSummary      `json:"personal_threads"`
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

	netWorth, err := a.repo.SumBalances(ctx, netWorthAccounts)
	if err != nil {
		return nil, fmt.Errorf("sum balances: %w", err)
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

	return &HouseholdDashboard{
		Period:           PeriodWindow{Key: period, From: from, To: to},
		NetWorth:         netWorth.String(),
		Income:           income.String(),
		Spending:         spending.String(),
		AccountCount:     len(netWorthAccounts),
		TransactionCount: txCount,
		ByCategory:       cats,
		LiveMemberCount:  len(live),
		InGraceCount:     len(inGrace),
	}, nil
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
		return nil, fmt.Errorf("list shared_budgets: %w", err)
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
		pace := decimal.Zero
		if !b.Amount.IsZero() {
			pace = spent.Div(b.Amount)
		}
		out = append(out, BudgetPaceItem{
			BudgetID:   b.ID,
			CategoryID: b.CategoryID,
			Period:     b.Period,
			Budget:     b.Amount.String(),
			Spent:      spent.String(),
			Pace:       pace.String(),
		})
	}
	return out, nil
}

// GoalProgress returns each shared_goal's progress ratio. No transaction
// queries — current_amount is canonical state.
func (a *Aggregator) GoalProgress(ctx context.Context, householdID int64) ([]GoalProgressItem, error) {
	if err := a.requireHousehold(ctx, householdID); err != nil {
		return nil, err
	}
	goals, err := a.repo.ListSharedGoals(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list shared_goals: %w", err)
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

	netWorth, err := a.repo.SumBalances(ctx, nwAccounts)
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
