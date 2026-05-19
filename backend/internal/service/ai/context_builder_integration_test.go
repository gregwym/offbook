package ai_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/ai"
)

// TestContextBuilder_Build_MixedFinancialData seeds a user with accounts,
// transactions across categories, a budget, a savings goal, and an
// investment snapshot, then asserts the built Context reflects each
// signal accurately.
//
// Skips when no test DB is configured — mirrors the existing service
// integration tests so `go test ./...` runs cleanly on a fresh checkout.
func TestContextBuilder_Build_MixedFinancialData(t *testing.T) {
	g := openTestDBForAI(t)
	userID := seedUser(t, g)

	// Seed: one checking account + one investment account, both owned by user.
	suffix := time.Now().Format("150405.000000")
	checking := &model.Account{
		UserID: userID, Name: "CtxChk-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
		Balance: decimal.NewFromInt(5000), // counted toward net worth
	}
	invAcct := &model.Account{
		UserID: userID, Name: "CtxInv-" + suffix, InstitutionSlug: "fixture",
		AccountType: "investment", Currency: "USD",
		Balance: decimal.Zero,
	}
	for _, a := range []*model.Account{checking, invAcct} {
		if err := g.Create(a).Error; err != nil {
			t.Fatalf("seed account: %v", err)
		}
	}

	groceries := &model.Category{Name: "CtxGroc-" + suffix, Slug: "ctx-groc-" + suffix}
	if err := g.Create(groceries).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	// t.Cleanup runs LIFO. Register the category cleanup FIRST so it runs
	// LAST — after the budget/etc. cleanup below has already cleared the
	// FK-bearing rows.
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, groceries.ID) })
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Investment{})
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Budget{})
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.SavingsGoal{})
		g.Unscoped().Delete(&model.Account{}, checking.ID)
		g.Unscoped().Delete(&model.Account{}, invAcct.ID)
	})

	// Two grocery outflows in the current month — total $80 spent.
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	mkTx := func(amt decimal.Decimal) {
		t.Helper()
		tx := &model.Transaction{
			UserID: userID, AccountID: checking.ID, CategoryID: &groceries.ID,
			Amount: amt, Currency: "USD",
			TransactionDate: now.AddDate(0, 0, -5), Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
	}
	mkTx(decimal.NewFromInt(-50))
	mkTx(decimal.NewFromInt(-30))

	// Budget: $200 groceries, monthly. With $80 spent → 40% utilized.
	budget := &model.Budget{
		UserID: userID, CategoryID: groceries.ID, Period: "monthly",
		Amount: decimal.NewFromInt(200), IsActive: true,
	}
	if err := g.Create(budget).Error; err != nil {
		t.Fatalf("seed budget: %v", err)
	}

	// Savings goal: $1000 target, $250 saved → 25% progress.
	goal := &model.SavingsGoal{
		UserID: userID, Name: "Emergency Fund",
		TargetAmount: decimal.NewFromInt(1000), CurrentAmount: decimal.NewFromInt(250),
	}
	if err := g.Create(goal).Error; err != nil {
		t.Fatalf("seed goal: %v", err)
	}

	// Investment: 10 shares VTI @ $250 market = $2500 in Equity asset class.
	ac := "Equity"
	mv := decimal.NewFromInt(2500)
	cb := decimal.NewFromInt(2000)
	inv := &model.Investment{
		UserID: userID, AccountID: invAcct.ID, Ticker: "VTI",
		AssetClass:   &ac,
		Quantity:     decimal.NewFromInt(10),
		MarketValue:  &mv,
		CostBasis:    &cb,
		SnapshotDate: now.AddDate(0, 0, -1),
		Source:       "manual",
	}
	if err := g.Create(inv).Error; err != nil {
		t.Fatalf("seed inv: %v", err)
	}

	dashSvc := service.NewDashboardService(repository.NewDashboardRepository(g))
	dashSvc.SetClock(func() time.Time { return now })
	budSvc := service.NewBudgetService(
		repository.NewBudgetRepository(g),
		repository.NewCategoryRepository(g),
	).WithNow(func() time.Time { return now })
	goalSvc := service.NewSavingsGoalService(
		repository.NewSavingsGoalRepository(g),
		repository.NewAccountRepository(g),
	)
	invSvc := service.NewInvestmentService(
		repository.NewInvestmentRepository(g),
		repository.NewAccountRepository(g),
	)
	catSvc := service.NewCategoryService(repository.NewCategoryRepository(g))

	cb_ := ai.NewContextBuilder(dashSvc, budSvc, goalSvc, invSvc, catSvc).WithNow(func() time.Time { return now })

	ctx, err := cb_.Build(context.Background(), userID)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// NetWorth = checking balance (investment account holds $0 in
	// accounts.balance; investment values aren't rolled into accounts).
	if ctx.NetWorth != "5000" {
		t.Errorf("NetWorth = %q, want 5000", ctx.NetWorth)
	}

	// Spend-by-category should include the grocery category at $80.
	foundGroceries := false
	for _, row := range ctx.SpendByCategory {
		if row.Category == groceries.Name {
			foundGroceries = true
			if row.Amount != "80" {
				t.Errorf("groceries spend = %q, want 80", row.Amount)
			}
		}
	}
	if !foundGroceries {
		t.Errorf("groceries category missing from SpendByCategory: %+v", ctx.SpendByCategory)
	}

	// Budget should appear with 40% utilization.
	if len(ctx.Budgets) != 1 {
		t.Fatalf("budgets len = %d, want 1: %+v", len(ctx.Budgets), ctx.Budgets)
	}
	bSnap := ctx.Budgets[0]
	if bSnap.Category != groceries.Name {
		t.Errorf("budget category = %q, want %q", bSnap.Category, groceries.Name)
	}
	if bSnap.Limit != "200" || bSnap.Spent != "80" {
		t.Errorf("budget limit/spent = %q/%q, want 200/80", bSnap.Limit, bSnap.Spent)
	}
	if bSnap.Pct < 0.39 || bSnap.Pct > 0.41 {
		t.Errorf("budget pct = %v, want ~0.40", bSnap.Pct)
	}

	// Savings goal: 25% progress.
	if len(ctx.SavingsGoals) != 1 {
		t.Fatalf("savings goals len = %d, want 1", len(ctx.SavingsGoals))
	}
	gSnap := ctx.SavingsGoals[0]
	if gSnap.Label != "Emergency Fund" {
		t.Errorf("goal label = %q, want Emergency Fund", gSnap.Label)
	}
	if gSnap.Target != "1000" || gSnap.Current != "250" {
		t.Errorf("goal target/current = %q/%q, want 1000/250", gSnap.Target, gSnap.Current)
	}
	if gSnap.ProgressPct < 0.24 || gSnap.ProgressPct > 0.26 {
		t.Errorf("goal progress = %v, want ~0.25", gSnap.ProgressPct)
	}

	// Holdings: $2500 market value in Equity → 100% weight.
	if ctx.Holdings.TotalMarketValue != "2500" {
		t.Errorf("holdings total = %q, want 2500", ctx.Holdings.TotalMarketValue)
	}
	if ctx.Holdings.HoldingsCount != 1 {
		t.Errorf("holdings count = %d, want 1", ctx.Holdings.HoldingsCount)
	}
	if len(ctx.Holdings.ByAssetClass) != 1 || ctx.Holdings.ByAssetClass[0].AssetClass != "Equity" {
		t.Fatalf("by_asset_class = %+v, want one Equity slice", ctx.Holdings.ByAssetClass)
	}
	if ctx.Holdings.ByAssetClass[0].WeightPct != "100" {
		t.Errorf("equity weight = %q, want 100", ctx.Holdings.ByAssetClass[0].WeightPct)
	}
}

// TestContextBuilder_Build_DifferentUser asserts that the builder scopes
// to the requested user — a second user's data must not leak into a
// build for the first.
func TestContextBuilder_Build_DifferentUser(t *testing.T) {
	g := openTestDBForAI(t)
	userA := seedUser(t, g)
	userB := seedUser(t, g)

	suffix := time.Now().Format("150405.000000")
	accB := &model.Account{
		UserID: userB, Name: "OtherCtx-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
		Balance: decimal.NewFromInt(9999),
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed other account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userB).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, accB.ID)
	})

	dashSvc := service.NewDashboardService(repository.NewDashboardRepository(g))
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	dashSvc.SetClock(func() time.Time { return now })

	cb := ai.NewContextBuilder(dashSvc, nil, nil, nil, nil).WithNow(func() time.Time { return now })
	ctxA, err := cb.Build(context.Background(), userA)
	if err != nil {
		t.Fatalf("Build for userA: %v", err)
	}
	// Net worth for userA must be 0 — userB's $9999 must not leak in.
	if ctxA.NetWorth != "0" {
		t.Errorf("userA NetWorth = %q, want 0 (userB's data leaked?)", ctxA.NetWorth)
	}
}

// --- helpers ---

func openTestDBForAI(t *testing.T) *gorm.DB {
	t.Helper()
	loadRepoDotenvForAI()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no DATABASE_URL set; skipping integration test")
	}
	g, err := db.Open(url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx, g); err != nil {
		t.Skipf("db.Ping: %v; skipping integration test", err)
	}
	return g
}

func loadRepoDotenvForAI() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func seedUser(t *testing.T, g *gorm.DB) int64 {
	t.Helper()
	u := &model.User{
		Email:        fmt.Sprintf("ai-test-%d-%d@example.test", time.Now().UnixNano(), len(t.Name())),
		PasswordHash: "x",
		LastScope:    model.ScopePersonal,
		DefaultScope: model.ScopePersonal,
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	return u.ID
}
