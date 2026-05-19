package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/gregwym/offbook/backend/internal/service"
)

// Context is the anonymized financial snapshot embedded in the system
// prompt before each user turn. Every field is an aggregate or a
// user-chosen label (category, budget period, goal label). The struct
// contains no holder names, account numbers, routing numbers, addresses,
// or any other PII — enforced by TestContext_NoPIIFieldNames in
// context_builder_test.go.
//
// Wire format is JSON. Decimals are stringified at the boundary so the
// LLM sees stable "1234.56" representations rather than scientific
// notation; the only float in the struct is ProgressPct (0.0–1.0+).
type Context struct {
	GeneratedAt     string           `json:"generated_at"`
	Period          ContextPeriod    `json:"period"`
	NetWorth        string           `json:"net_worth"`
	SpendByCategory []SpendCategory  `json:"spend_by_category"`
	Budgets         []BudgetSnapshot `json:"budgets"`
	SavingsGoals    []GoalSnapshot   `json:"savings_goals"`
	Holdings        HoldingsSummary  `json:"holdings"`
}

// ContextPeriod is the [from, to) window summarized by SpendByCategory.
// RFC3339 dates so the LLM sees consistent timestamps.
type ContextPeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// SpendCategory is one row of the spend rollup. "Category" is the
// user-visible category label (e.g. "Groceries"), never PII.
type SpendCategory struct {
	Category string `json:"category"`
	Amount   string `json:"amount"`
}

// BudgetSnapshot is one row of the budget envelope view. Pct is a
// fraction in [0, 1+) — the assistant can multiply by 100 to display
// as a percentage; we keep BudgetService's canonical 0..1 shape.
type BudgetSnapshot struct {
	Category string  `json:"category"`
	Period   string  `json:"period"`
	Limit    string  `json:"limit"`
	Spent    string  `json:"spent"`
	Pct      float64 `json:"pct"`
}

// GoalSnapshot is one row of the savings-goal view. The user-chosen
// label sits in Label rather than Name so the privacy test's "name"
// substring check stays strict.
type GoalSnapshot struct {
	Label       string  `json:"label"`
	Target      string  `json:"target"`
	Current     string  `json:"current"`
	ProgressPct float64 `json:"progress_pct"`
}

// HoldingsSummary collapses the portfolio to totals + asset-class
// allocation. Deliberately omits per-row tickers — the assistant gets the
// shape of the portfolio, not the contents.
type HoldingsSummary struct {
	TotalMarketValue string             `json:"total_market_value"`
	TotalCostBasis   string             `json:"total_cost_basis"`
	HoldingsCount    int                `json:"holdings_count"`
	ByAssetClass     []AssetClassWeight `json:"by_asset_class"`
}

// AssetClassWeight is one slice of the allocation donut. WeightPct is a
// 0–100 percentage to match the API-side struct, not a fraction.
type AssetClassWeight struct {
	AssetClass  string `json:"asset_class"`
	MarketValue string `json:"market_value"`
	WeightPct   string `json:"weight_pct"`
}

// ContextBuilder assembles an anonymized context from existing aggregate
// services. The set of injected dependencies is the architectural
// guarantee: there is no pii_repo here, and there cannot be — pii_service
// isn't on the dependency list either.
//
// All five services are required in production; nil values are accepted
// to make targeted tests easier (a nil service is treated as "no data of
// that kind" so a test focused on, say, budgets isn't forced to seed
// investments).
type ContextBuilder struct {
	dashboard   *service.DashboardService
	budgets     *service.BudgetService
	goals       *service.SavingsGoalService
	investments *service.InvestmentService
	categories  *service.CategoryService
	now         func() time.Time

	// SpendMonths is the trailing window summed into SpendByCategory.
	// Default 3 — enough to spot trend changes without overwhelming the
	// prompt budget.
	SpendMonths int
}

// NewContextBuilder wires the builder. Pass nil for any service you don't
// have in a test; production callers pass all five.
func NewContextBuilder(
	dashboard *service.DashboardService,
	budgets *service.BudgetService,
	goals *service.SavingsGoalService,
	investments *service.InvestmentService,
	categories *service.CategoryService,
) *ContextBuilder {
	return &ContextBuilder{
		dashboard:   dashboard,
		budgets:     budgets,
		goals:       goals,
		investments: investments,
		categories:  categories,
		now:         time.Now,
		SpendMonths: 3,
	}
}

// WithNow overrides the clock for tests. Returns the receiver for chaining.
func (b *ContextBuilder) WithNow(fn func() time.Time) *ContextBuilder {
	b.now = fn
	return b
}

// Build returns the anonymized context for the given user. Partial
// failures are tolerated — a missing investment account, for example,
// returns an empty HoldingsSummary rather than aborting the build.
func (b *ContextBuilder) Build(ctx context.Context, userID int64) (*Context, error) {
	now := b.now()
	from, to := trailingMonthsWindow(now, b.SpendMonths)

	out := &Context{
		GeneratedAt:     now.UTC().Format(time.RFC3339),
		Period:          ContextPeriod{From: from.UTC().Format(time.RFC3339), To: to.UTC().Format(time.RFC3339)},
		NetWorth:        "0",
		SpendByCategory: []SpendCategory{},
		Budgets:         []BudgetSnapshot{},
		SavingsGoals:    []GoalSnapshot{},
		Holdings: HoldingsSummary{
			TotalMarketValue: "0",
			TotalCostBasis:   "0",
			ByAssetClass:     []AssetClassWeight{},
		},
	}

	if b.dashboard != nil {
		// Net worth is point-in-time, so the dashboard's MTD period works
		// fine — the NetWorth field on the summary ignores the period.
		summary, err := b.dashboard.Summarize(ctx, userID, service.PeriodCurrentMonth)
		if err != nil {
			return nil, fmt.Errorf("ai: dashboard summarize: %w", err)
		}
		out.NetWorth = summary.NetWorth

		rows, err := b.dashboard.SpendByCategory(ctx, userID, from, to)
		if err != nil {
			return nil, fmt.Errorf("ai: spend by category: %w", err)
		}
		for _, r := range rows {
			out.SpendByCategory = append(out.SpendByCategory, SpendCategory{
				Category: r.Name,
				Amount:   r.Amount,
			})
		}
	}

	if b.budgets != nil {
		list, err := b.budgets.List(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("ai: list budgets: %w", err)
		}
		labels := b.categoryLabels(ctx)
		for _, bud := range list {
			if !bud.IsActive {
				continue
			}
			// One round trip per active budget. Acceptable: budgets are
			// few (single-digit) and the context builder runs per-thread,
			// not per-token.
			spend, err := b.budgets.Spend(ctx, userID, bud.ID)
			if err != nil {
				return nil, fmt.Errorf("ai: budget spend %d: %w", bud.ID, err)
			}
			out.Budgets = append(out.Budgets, BudgetSnapshot{
				Category: labels[bud.CategoryID],
				Period:   bud.Period,
				Limit:    spend.Limit.String(),
				Spent:    spend.Spent.String(),
				Pct:      spend.Pct,
			})
		}
	}

	if b.goals != nil {
		gs, err := b.goals.List(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("ai: list goals: %w", err)
		}
		for i := range gs {
			v := service.View(&gs[i])
			out.SavingsGoals = append(out.SavingsGoals, GoalSnapshot{
				Label:       v.SavingsGoal.Name,
				Target:      v.TargetAmount.String(),
				Current:     v.CurrentAmount.String(),
				ProgressPct: v.ProgressPct,
			})
		}
	}

	if b.investments != nil {
		pf, err := b.investments.PortfolioSummary(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("ai: portfolio summary: %w", err)
		}
		out.Holdings.TotalMarketValue = pf.TotalMarketValue.String()
		out.Holdings.TotalCostBasis = pf.TotalCostBasis.String()
		out.Holdings.HoldingsCount = pf.HoldingsCount
		for _, ac := range pf.ByAssetClass {
			out.Holdings.ByAssetClass = append(out.Holdings.ByAssetClass, AssetClassWeight{
				AssetClass:  ac.AssetClass,
				MarketValue: ac.MarketValue.String(),
				WeightPct:   ac.WeightPct.String(),
			})
		}
	}

	return out, nil
}

// categoryLabels returns a category-id → label map for budget rendering.
// Empty map (rather than error) when the category service is missing or
// errors — labels are nice-to-have; budget data is the load-bearing
// signal.
func (b *ContextBuilder) categoryLabels(ctx context.Context) map[int64]string {
	if b.categories == nil {
		return map[int64]string{}
	}
	cats, err := b.categories.List(ctx)
	if err != nil {
		return map[int64]string{}
	}
	out := make(map[int64]string, len(cats))
	for _, c := range cats {
		out[c.ID] = c.Name
	}
	return out
}

// trailingMonthsWindow returns the [first-of-month-N-ago, today+1day)
// window summed into SpendByCategory. End is exclusive to match the
// dashboard repo's convention.
func trailingMonthsWindow(now time.Time, months int) (time.Time, time.Time) {
	if months <= 0 {
		months = 1
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	from := time.Date(now.Year(), now.Month()-time.Month(months-1), 1, 0, 0, 0, 0, now.Location())
	to := today.AddDate(0, 0, 1)
	return from, to
}
