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

// TestCategoryService_DeleteMerge covers #575: deleting or merging a user's
// own category moves its transactions, split lines, rules, budgets and
// subcategories in one transaction, and spending, budget actuals and the
// drill-down total still agree to the cent afterwards.
func TestCategoryService_DeleteMerge(t *testing.T) {
	ctx := context.Background()
	g := openTestDB(t)
	catRepo := repository.NewCategoryRepository(g)
	svc := service.NewCategoryService(catRepo).WithDB(g)
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	budgets := service.NewBudgetService(repository.NewBudgetRepository(g), catRepo).WithNow(func() time.Time { return now })
	dash := service.NewDashboardService(
		repository.NewDashboardRepository(g),
		repository.NewTransactionRepository(g),
		repository.NewUserRepository(g),
		valuation.NewService(
			repository.NewPositionRepository(g),
			repository.NewPriceRepository(g),
			repository.NewAssetRepository(g),
			repository.NewAccountRepository(g),
		),
	)
	dash.SetClock(func() time.Time { return now })
	txRepo := repository.NewTransactionRepository(g)

	alice := seedTestUser(t, g)
	bob := seedTestUser(t, g)
	t.Cleanup(func() { g.Exec("DELETE FROM ai_categorization_verdicts WHERE user_id IN (?, ?)", alice, bob) })
	acc := &model.Account{UserID: alice, Name: "Checking", InstitutionSlug: "fixture", AccountType: "checking"}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	groceries := systemCategoryID(t, "groceries")

	mkCat := func(user int64, name string, parent *int64) *model.Category {
		t.Helper()
		c, err := svc.Create(ctx, user, service.CreateCategoryInput{Name: name, ParentID: parent})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return c
	}
	coffee := mkCat(alice, "Coffee", nil)
	espresso := mkCat(alice, "Espresso", &coffee.ID)
	cafes := mkCat(alice, "Cafes", nil)
	bobs := mkCat(bob, "Bob's", nil)

	rule := &model.CategorizationRule{UserID: alice, Pattern: "STARBUCKS", MatchType: "contains", CategoryID: &coffee.ID, IsActive: true}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	day := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	amt := func(s string) func(*model.Transaction) {
		return func(tx *model.Transaction) { tx.Amount = decimal.RequireFromString(s); tx.TransactionDate = day }
	}
	manual := mkTxn(t, g, alice, acc.ID, amt("-12.34"), withManualCategory(coffee.ID))
	byRule := mkTxn(t, g, alice, acc.ID, amt("-7.66"), func(tx *model.Transaction) {
		m := "rule"
		tx.CategoryID, tx.CategorizationMethod, tx.CategorizationRuleID = &coffee.ID, &m, &rule.ID
	})
	split := mkTxn(t, g, alice, acc.ID, amt("-40"))
	if err := g.Exec(`INSERT INTO transaction_splits (transaction_id, user_id, category_id, amount) VALUES
		(?, ?, ?, -25), (?, ?, ?, -15)`, split.ID, alice, coffee.ID, split.ID, alice, groceries).Error; err != nil {
		t.Fatalf("seed splits: %v", err)
	}
	mkTxn(t, g, alice, acc.ID, amt("-5"), withManualCategory(cafes.ID))
	if err := g.Exec(`INSERT INTO ai_categorization_verdicts (user_id, merchant_key, category_id, confidence, provider)
		VALUES (?, 'starbucks', ?, 0.9, 'test')`, alice, coffee.ID).Error; err != nil {
		t.Fatalf("seed verdict: %v", err)
	}
	mkBudget := func(cat int64, amount int64) *model.Budget {
		t.Helper()
		b, err := budgets.Create(ctx, repository.UserOwner(alice), service.CreateBudgetInput{
			CategoryID: cat, Period: "monthly", Amount: decimal.NewFromInt(amount)})
		if err != nil {
			t.Fatalf("seed budget: %v", err)
		}
		return b
	}
	coffeeBudget := mkBudget(coffee.ID, 50)
	cafesBudget := mkBudget(cafes.ID, 30)

	spendTotal := func(cat int64) decimal.Decimal {
		t.Helper()
		from, to := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		v, err := txRepo.SpendTotal(ctx, alice, repository.TransactionFilter{Spending: true, CategoryID: &cat, From: &from, To: &to})
		if err != nil {
			t.Fatalf("SpendTotal: %v", err)
		}
		return v
	}
	byCategory := func() (map[int64]decimal.Decimal, decimal.Decimal) {
		t.Helper()
		items, err := dash.SpendByCategory(ctx, alice, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("SpendByCategory: %v", err)
		}
		out, sum := map[int64]decimal.Decimal{}, decimal.Zero
		for _, it := range items {
			v := decimal.RequireFromString(it.Amount)
			sum = sum.Add(v)
			if it.CategoryID != nil {
				out[*it.CategoryID] = v
			}
		}
		return out, sum
	}
	_, totalBefore := byCategory()

	t.Run("usage counts what references the category", func(t *testing.T) {
		u, err := svc.Usage(ctx, alice, coffee.ID)
		if err != nil {
			t.Fatalf("Usage: %v", err)
		}
		want := repository.CategoryUsage{Transactions: 2, Splits: 1, Rules: 1, Budgets: 1, Subcategories: 1}
		if u != want {
			t.Errorf("usage = %+v, want %+v", u, want)
		}
	})

	errCases := []struct {
		name        string
		user, id    int64
		replaceWith *int64
		want        error
	}{
		{"built-in category", alice, groceries, nil, service.ErrCategoryReadOnly},
		{"another user's category", alice, bobs.ID, nil, service.ErrCategoryNotFound},
		{"bob deletes alice's", bob, coffee.ID, nil, service.ErrCategoryNotFound},
		{"merge into itself", alice, coffee.ID, &coffee.ID, service.ErrInvalidReplacement},
		{"merge into another user's", alice, coffee.ID, &bobs.ID, service.ErrInvalidReplacement},
	}
	for _, tc := range errCases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if _, err := svc.Delete(ctx, tc.user, tc.id, tc.replaceWith); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("usage of a built-in category is read-only", func(t *testing.T) {
		if _, err := svc.Usage(ctx, alice, groceries); !errors.Is(err, service.ErrCategoryReadOnly) {
			t.Errorf("err = %v, want ErrCategoryReadOnly", err)
		}
	})

	t.Run("merge Coffee into Cafes", func(t *testing.T) {
		moved, err := svc.Delete(ctx, alice, coffee.ID, &cafes.ID)
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if moved.Transactions != 2 || moved.Splits != 1 || moved.Rules != 1 || moved.Budgets != 1 {
			t.Errorf("moved = %+v", moved)
		}

		list, _ := svc.List(ctx, alice)
		if hasCategory(list, coffee.ID) {
			t.Error("Coffee still listed after the merge")
		}
		var row model.Transaction
		g.First(&row, manual.ID)
		if *row.CategoryID != cafes.ID || *row.CategorizationMethod != "manual" {
			t.Errorf("manual row = cat %v method %v, want Cafes/manual", *row.CategoryID, *row.CategorizationMethod)
		}
		row = model.Transaction{}
		g.First(&row, byRule.ID)
		if *row.CategoryID != cafes.ID || *row.CategorizationMethod != "rule" || row.CategorizationRuleID == nil || *row.CategorizationRuleID != rule.ID {
			t.Errorf("rule row = %+v, want Cafes, method rule, rule kept", row)
		}
		var r model.CategorizationRule
		g.First(&r, rule.ID)
		if *r.CategoryID != cafes.ID || !r.IsActive {
			t.Errorf("rule = cat %v active %v, want Cafes and active", *r.CategoryID, r.IsActive)
		}
		var esp model.Category
		g.First(&esp, espresso.ID)
		if esp.ParentID == nil || *esp.ParentID != cafes.ID {
			t.Errorf("Espresso parent = %v, want Cafes", esp.ParentID)
		}
		var verdictCat int64
		g.Raw("SELECT category_id FROM ai_categorization_verdicts WHERE user_id = ?", alice).Scan(&verdictCat)
		if verdictCat != cafes.ID {
			t.Errorf("AI verdict category = %d, want Cafes", verdictCat)
		}

		// The Coffee budget folds into the Cafes budget for the same period.
		if _, err := budgets.Get(ctx, repository.UserOwner(alice), coffeeBudget.ID); err == nil {
			t.Error("Coffee budget still live")
		}
		spend, err := budgets.Spend(ctx, alice, cafesBudget.ID)
		if err != nil {
			t.Fatalf("Spend: %v", err)
		}
		want := decimal.RequireFromString("50") // 12.34 + 7.66 + 25 (split line) + 5
		if !spend.Limit.Equal(decimal.NewFromInt(80)) || !spend.Spent.Equal(want) {
			t.Errorf("Cafes budget limit %s spent %s, want 80 / %s", spend.Limit, spend.Spent, want)
		}
		cats, total := byCategory()
		if !cats[cafes.ID].Equal(want) || !total.Equal(totalBefore) {
			t.Errorf("by-category Cafes %s total %s, want %s / %s", cats[cafes.ID], total, want, totalBefore)
		}
		if got := spendTotal(cafes.ID); !got.Equal(want) {
			t.Errorf("drill-down total %s, want %s", got, want)
		}
		if _, err := svc.Delete(ctx, alice, coffee.ID, nil); !errors.Is(err, service.ErrCategoryNotFound) {
			t.Errorf("second delete err = %v, want ErrCategoryNotFound", err)
		}
		// The name is free again.
		mkCat(alice, "Coffee", nil)
	})

	t.Run("delete Cafes to Uncategorized", func(t *testing.T) {
		tagOnly := &model.CategorizationRule{UserID: alice, Pattern: "PEETS", MatchType: "contains", CategoryID: &cafes.ID, IsActive: true, AddTags: model.Tags{"coffee"}}
		if err := g.Create(tagOnly).Error; err != nil {
			t.Fatalf("seed rule: %v", err)
		}
		if _, err := svc.Delete(ctx, alice, cafes.ID, nil); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		var row model.Transaction
		g.First(&row, byRule.ID)
		if row.CategoryID != nil || row.CategorizationMethod != nil || row.CategorizationRuleID != nil {
			t.Errorf("row = %+v, want uncategorized with no provenance", row)
		}
		var r model.CategorizationRule
		g.First(&r, rule.ID)
		if r.CategoryID != nil || r.IsActive {
			t.Errorf("category-only rule = cat %v active %v, want none and off", r.CategoryID, r.IsActive)
		}
		r = model.CategorizationRule{}
		g.First(&r, tagOnly.ID)
		if r.CategoryID != nil || !r.IsActive {
			t.Errorf("tagging rule = cat %v active %v, want none and still on", r.CategoryID, r.IsActive)
		}
		if _, err := budgets.Get(ctx, repository.UserOwner(alice), cafesBudget.ID); err == nil {
			t.Error("Cafes budget still live")
		}
		var verdicts int64
		g.Raw("SELECT count(*) FROM ai_categorization_verdicts WHERE user_id = ?", alice).Scan(&verdicts)
		if verdicts != 0 {
			t.Errorf("verdicts = %d, want 0", verdicts)
		}
		var esp model.Category
		g.First(&esp, espresso.ID)
		if esp.ParentID != nil {
			t.Errorf("Espresso parent = %v, want top-level", *esp.ParentID)
		}
		var splitCat *int64
		g.Raw("SELECT category_id FROM transaction_splits WHERE transaction_id = ? AND amount = -25", split.ID).Scan(&splitCat)
		if splitCat != nil {
			t.Errorf("split line category = %d, want uncategorized", *splitCat)
		}
		if _, total := byCategory(); !total.Equal(totalBefore) {
			t.Errorf("spending total %s, want %s", total, totalBefore)
		}
		from, to := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		uncat, err := txRepo.SpendTotal(ctx, alice, repository.TransactionFilter{Spending: true, UncategorizedOnly: true, From: &from, To: &to})
		if err != nil || !uncat.Equal(decimal.NewFromInt(50)) {
			t.Errorf("uncategorized drill-down %s (err %v), want 50", uncat, err)
		}
	})

	t.Run("bob's category is untouched", func(t *testing.T) {
		var c model.Category
		if err := g.First(&c, bobs.ID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			t.Error("bob's category was deleted")
		}
	})
}
