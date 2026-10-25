package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestBudgetService_GroupBudget_CoversSubcategories (#596): a budget on a
// category group counts its subcategories' spending, except a subcategory
// with an active budget of its own, which counts only there. The card, the
// rollover carry, the dashboard alert and the month summary all agree, and no
// dollar is counted twice.
func TestBudgetService_GroupBudget_CoversSubcategories(t *testing.T) {
	svc, userID, food, g := newBudgetSvc(t)
	ctx := context.Background()
	groceries := seedSummaryCategory(t, g, "grp-groceries", &food)
	dining := seedSummaryCategory(t, g, "grp-dining", &food)
	coffee := seedSummaryCategory(t, g, "grp-coffee", &food)

	acc := &model.Account{UserID: userID, Name: "Group Checking", InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD"}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	seed := func(cat int64, amount string, on time.Time) {
		t.Helper()
		tx := &model.Transaction{
			UserID: userID, AccountID: acc.ID, CategoryID: &cat, Amount: decimal.RequireFromString(amount),
			TransactionDate: on, Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
	}
	april := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	may := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	seed(food, "-10.00", may)
	seed(groceries, "-120.50", may)
	seed(dining, "-60.25", may)
	seed(dining, "5.25", may) // refund nets against dining
	seed(coffee, "-30.00", may)
	seed(groceries, "-100.00", april)

	owner := repository.UserOwner(userID)
	group, err := svc.Create(ctx, owner, service.CreateBudgetInput{CategoryID: food, Period: "monthly", Amount: decimal.NewFromInt(200)})
	if err != nil {
		t.Fatalf("create group budget: %v", err)
	}
	coffeeBudget, err := svc.Create(ctx, owner, service.CreateBudgetInput{CategoryID: coffee, Period: "monthly", Amount: decimal.NewFromInt(40)})
	if err != nil {
		t.Fatalf("create coffee budget: %v", err)
	}
	on := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	t.Run("group card counts subcategories without their own budget", func(t *testing.T) {
		card, err := svc.SpendOn(ctx, userID, group.ID, on)
		if err != nil {
			t.Fatalf("SpendOn: %v", err)
		}
		// 10 + 120.50 + 60.25 - 5.25; coffee has its own budget.
		if want := decimal.RequireFromString("185.50"); !card.Spent.Equal(want) {
			t.Errorf("group spent = %s, want %s", card.Spent, want)
		}
		got := map[int64]bool{}
		for _, id := range card.CategoryIDs {
			got[id] = true
		}
		if len(card.CategoryIDs) != 3 || card.CategoryIDs[0] != food || !got[groceries] || !got[dining] || got[coffee] {
			t.Errorf("group covers %v, want [food groceries dining] (food %d, coffee %d excluded)", card.CategoryIDs, food, coffee)
		}
		child, err := svc.SpendOn(ctx, userID, coffeeBudget.ID, on)
		if err != nil {
			t.Fatalf("SpendOn coffee: %v", err)
		}
		if !child.Spent.Equal(decimal.NewFromInt(30)) || len(child.CategoryIDs) != 1 {
			t.Errorf("coffee spent = %s over %v, want 30 over [coffee]", child.Spent, child.CategoryIDs)
		}
	})

	t.Run("month summary counts each dollar once", func(t *testing.T) {
		sum, err := svc.MonthSummary(ctx, userID, on)
		if err != nil {
			t.Fatalf("MonthSummary: %v", err)
		}
		if want := decimal.RequireFromString("215.50"); !sum.SpentInBudgets.Equal(want) {
			t.Errorf("spent in budgets = %s, want %s", sum.SpentInBudgets, want)
		}
		if !sum.NotInBudget.IsZero() || len(sum.Unbudgeted) != 0 {
			t.Errorf("not in budget = %s over %d rows, want 0", sum.NotInBudget, len(sum.Unbudgeted))
		}
		if !sum.SpentInBudgets.Add(sum.NotInBudget).Equal(sum.TotalSpending) {
			t.Errorf("spent in budgets + not in a budget = %s, want total %s", sum.SpentInBudgets.Add(sum.NotInBudget), sum.TotalSpending)
		}
	})

	t.Run("alert uses the group's covered spend", func(t *testing.T) {
		alerts, err := svc.Alerts(ctx, userID)
		if err != nil {
			t.Fatalf("Alerts: %v", err)
		}
		var found bool
		for _, a := range alerts {
			if a.BudgetID != group.ID {
				continue
			}
			found = true
			if !a.Spent.Equal(decimal.RequireFromString("185.50")) || a.Severity != service.AlertWarning {
				t.Errorf("group alert = %s %s, want 185.50 warning", a.Spent, a.Severity)
			}
		}
		if !found {
			t.Errorf("no alert for the group budget at 92.75%%")
		}
	})

	t.Run("pausing the child's budget moves its spend to the group", func(t *testing.T) {
		off := false
		if _, err := svc.Update(ctx, owner, coffeeBudget.ID, service.UpdateBudgetInput{IsActive: &off}); err != nil {
			t.Fatalf("pause coffee: %v", err)
		}
		card, err := svc.SpendOn(ctx, userID, group.ID, on)
		if err != nil {
			t.Fatalf("SpendOn: %v", err)
		}
		if want := decimal.RequireFromString("215.50"); !card.Spent.Equal(want) {
			t.Errorf("group spent = %s, want %s", card.Spent, want)
		}
	})

	t.Run("rollover carries the group's covered spend", func(t *testing.T) {
		g.Model(&model.Budget{}).Where("id = ?", group.ID).Update("created_at", april)
		yes := true
		if _, err := svc.Update(ctx, owner, group.ID, service.UpdateBudgetInput{Rollover: &yes}); err != nil {
			t.Fatalf("rollover on: %v", err)
		}
		card, err := svc.SpendOn(ctx, userID, group.ID, on)
		if err != nil {
			t.Fatalf("SpendOn: %v", err)
		}
		// April: limit 200 - groceries 100 = 100 carried into May.
		if want := decimal.NewFromInt(100); !card.Carryover.Equal(want) {
			t.Errorf("carryover = %s, want %s", card.Carryover, want)
		}
	})
}
