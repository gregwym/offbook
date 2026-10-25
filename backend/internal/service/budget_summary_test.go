package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// seedSummaryCategory creates a throwaway category, optionally under parent.
func seedSummaryCategory(t *testing.T, g *gorm.DB, name string, parent *int64) int64 {
	t.Helper()
	c := &model.Category{Name: name, Slug: "sum-" + name + "-" + time.Now().Format("150405.000000"), ParentID: parent}
	if err := g.Create(c).Error; err != nil {
		t.Fatalf("seed category %s: %v", name, err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, c.ID) })
	return c.ID
}

// TestBudgetService_MonthSummary_AddsUpToInsightsSpending (#589): budgeted,
// spent-in-budgets and not-in-a-budget for May, with spending in budgeted,
// unbudgeted, child and no category, plus a refund, a transfer, a hidden row
// and an April row. Spent in budgets + not in a budget equals the Insights
// spending total exactly, and only active monthly budgets count. A child whose
// own budget is paused counts in its group's budget (#596).
func TestBudgetService_MonthSummary_AddsUpToInsightsSpending(t *testing.T) {
	svc, userID, groceries, g := newBudgetSvc(t)
	ctx := context.Background()
	dining := seedSummaryCategory(t, g, "dining", nil)
	snacks := seedSummaryCategory(t, g, "snacks", &groceries)
	gifts := seedSummaryCategory(t, g, "gifts", nil)

	acc := &model.Account{UserID: userID, Name: "Summary Checking", InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD"}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	may := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	seed := func(cat *int64, amount string, on time.Time, transfer bool) *model.Transaction {
		t.Helper()
		tx := &model.Transaction{
			UserID: userID, AccountID: acc.ID, CategoryID: cat, Amount: decimal.RequireFromString(amount),
			TransactionDate: on, Source: "manual", IsTransfer: transfer,
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
		return tx
	}
	seed(&groceries, "-40.00", may, false)
	seed(&groceries, "-60.25", may, false)
	seed(&groceries, "10.00", may, false) // refund nets against groceries
	seed(&dining, "-500.10", may, false)
	seed(&snacks, "-20.00", may, false) // child of groceries, paused budget: counts in groceries (#596)
	seed(nil, "-7.77", may, false)      // uncategorized
	seed(nil, "3000.00", may, false)    // income, not spending
	seed(&dining, "-1000.00", may, true)
	hidden := seed(&dining, "-300.00", may, false)
	if err := g.Model(&model.Transaction{}).Where("id = ?", hidden.ID).Update("hidden_at", may).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	seed(&groceries, "-999.00", time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), false)

	owner := repository.UserOwner(userID)
	mk := func(cat int64, period, amount string) *model.Budget {
		t.Helper()
		b, err := svc.Create(ctx, owner, service.CreateBudgetInput{CategoryID: cat, Period: period, Amount: decimal.RequireFromString(amount)})
		if err != nil {
			t.Fatalf("create budget: %v", err)
		}
		return b
	}
	groceriesBudget := mk(groceries, "monthly", "300")
	mk(gifts, "monthly", "100") // budgeted, nothing spent
	mk(dining, "weekly", "50")  // not a monthly budget: dining stays unbudgeted
	paused := mk(snacks, "monthly", "25")
	off := false
	if _, err := svc.Update(ctx, owner, paused.ID, service.UpdateBudgetInput{IsActive: &off}); err != nil {
		t.Fatalf("pause: %v", err)
	}

	sum, err := svc.MonthSummary(ctx, userID, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MonthSummary: %v", err)
	}
	checks := []struct {
		name string
		got  decimal.Decimal
		want string
	}{
		{"budgeted", sum.Budgeted, "400"},
		{"available", sum.Available, "400"},
		{"spent_in_budgets", sum.SpentInBudgets, "110.25"},
		{"remaining", sum.Remaining, "289.75"},
		{"not_in_budget", sum.NotInBudget, "507.87"},
		{"total_spending", sum.TotalSpending, "618.12"},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !c.got.Equal(decimal.RequireFromString(c.want)) {
				t.Errorf("%s = %s, want %s", c.name, c.got, c.want)
			}
		})
	}
	if sum.Month != "2026-05" || sum.BudgetCount != 2 {
		t.Errorf("month/count = %s/%d, want 2026-05/2", sum.Month, sum.BudgetCount)
	}
	if !sum.SpentInBudgets.Add(sum.NotInBudget).Equal(sum.TotalSpending) {
		t.Errorf("spent in budgets + not in a budget = %s, want total %s", sum.SpentInBudgets.Add(sum.NotInBudget), sum.TotalSpending)
	}
	breakdown := decimal.Zero
	for _, u := range sum.Unbudgeted {
		breakdown = breakdown.Add(u.Spent)
		if u.CategoryID != nil && (*u.CategoryID == groceries || *u.CategoryID == snacks) {
			t.Errorf("budgeted category %d listed as unbudgeted", *u.CategoryID)
		}
	}
	if !breakdown.Equal(sum.NotInBudget) || len(sum.Unbudgeted) != 2 {
		t.Errorf("unbudgeted breakdown = %s over %d rows, want %s over 2", breakdown, len(sum.Unbudgeted), sum.NotInBudget)
	}
	if sum.Unbudgeted[0].CategoryID == nil || *sum.Unbudgeted[0].CategoryID != dining {
		t.Errorf("largest unbudgeted category = %v, want dining", sum.Unbudgeted[0].CategoryID)
	}

	// The same month on Insights.
	dash := service.NewDashboardService(
		repository.NewDashboardRepository(g), repository.NewTransactionRepository(g), repository.NewUserRepository(g),
		valuation.NewService(repository.NewPositionRepository(g), repository.NewPriceRepository(g), repository.NewAssetRepository(g), repository.NewAccountRepository(g)),
	)
	dash.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	insights, err := dash.Summarize(ctx, userID, "current_month")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if insights.Spending != sum.TotalSpending.String() {
		t.Errorf("Insights spending = %s, summary total = %s", insights.Spending, sum.TotalSpending)
	}

	// The summary matches the groceries card.
	card, err := svc.SpendOn(ctx, userID, groceriesBudget.ID, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SpendOn: %v", err)
	}
	if !card.Spent.Equal(decimal.RequireFromString("110.25")) {
		t.Errorf("groceries card spent = %s, want 110.25", card.Spent)
	}
}

// TestBudgetService_MonthSummary_PastMonthKeepsItsLimits: after a limit edit
// (#580) April's Budgeted total doesn't change, May's does, and a future
// month is refused.
func TestBudgetService_MonthSummary_PastMonthKeepsItsLimits(t *testing.T) {
	svc, userID, groceries, _ := newBudgetSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)
	b, err := svc.Create(ctx, owner, service.CreateBudgetInput{CategoryID: groceries, Period: "monthly", Amount: decimal.NewFromInt(300)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	april := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	before, err := svc.MonthSummary(ctx, userID, april)
	if err != nil {
		t.Fatalf("April before edit: %v", err)
	}
	amt := decimal.NewFromInt(350)
	if _, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{Amount: &amt}); err != nil {
		t.Fatalf("edit limit: %v", err)
	}
	after, err := svc.MonthSummary(ctx, userID, april)
	if err != nil {
		t.Fatalf("April after edit: %v", err)
	}
	if !after.Budgeted.Equal(before.Budgeted) || !after.Budgeted.Equal(decimal.NewFromInt(300)) {
		t.Errorf("April budgeted = %s after edit (before %s), want 300", after.Budgeted, before.Budgeted)
	}
	may, err := svc.MonthSummary(ctx, userID, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("May: %v", err)
	}
	if !may.Budgeted.Equal(amt) {
		t.Errorf("May budgeted = %s, want 350", may.Budgeted)
	}
	if _, err := svc.MonthSummary(ctx, userID, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, service.ErrBudgetDateInFuture) {
		t.Errorf("future month err = %v, want ErrBudgetDateInFuture", err)
	}
}

// TestBudgetService_MonthSummary_TenantIsolation: another user's budgets and
// spending never reach the summary.
func TestBudgetService_MonthSummary_TenantIsolation(t *testing.T) {
	svc, userA, groceries, g := newBudgetSvc(t)
	ctx := context.Background()
	userB := seedTestUser(t, g)
	acc := &model.Account{UserID: userB, Name: "B Checking", InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD"}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
		g.Unscoped().Where("user_id = ?", userB).Delete(&model.Budget{})
	})
	cat := groceries
	if err := g.Create(&model.Transaction{UserID: userB, AccountID: acc.ID, CategoryID: &cat, Amount: decimal.NewFromInt(-80),
		TransactionDate: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Source: "manual"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Create(ctx, repository.UserOwner(userB), service.CreateBudgetInput{CategoryID: groceries, Period: "monthly", Amount: decimal.NewFromInt(500)}); err != nil {
		t.Fatalf("B budget: %v", err)
	}
	sum, err := svc.MonthSummary(ctx, userA, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MonthSummary: %v", err)
	}
	if !sum.Budgeted.IsZero() || !sum.TotalSpending.IsZero() || sum.BudgetCount != 0 || len(sum.Unbudgeted) != 0 {
		t.Errorf("user A summary leaked user B data: %+v", sum)
	}
}
