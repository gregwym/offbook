package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestSpending_ProposedTransferPairExcluded is the #447 regression: two
// opposite-amount rows in different accounts within the match window are a
// *proposed* transfer (#371). Until the user decides, they must not inflate
// income, spending, category or top-merchant figures; unlinking nothing and
// the rows staying unconfirmed keeps them out, while a lone row (no partner)
// still counts.
func TestSpending_ProposedTransferPairExcluded(t *testing.T) {
	dash, userID, checkingID, g := newDashboardSvc(t)
	ctx := context.Background()

	savings := &model.Account{
		UserID: userID, Name: "Savings-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "savings", Currency: "USD",
	}
	if err := g.Create(savings).Error; err != nil {
		t.Fatalf("seed savings: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", savings.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, savings.ID)
	})

	d := func(day int) time.Time { return time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC) }
	seed := func(acct int64, day int, amt, desc string) {
		tx := &model.Transaction{
			UserID: userID, AccountID: acct, Amount: decimal.RequireFromString(amt),
			Description: &desc, TransactionDate: d(day), Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	seed(checkingID, 1, "3000.00", "Payroll")
	seed(checkingID, 3, "-82.15", "Whole Foods")
	seed(checkingID, 5, "-1500.00", "Landlord")
	seed(checkingID, 9, "-500.00", "Transfer to Savings")
	seed(savings.ID, 10, "500.00", "Transfer from Checking") // 1 day later, still in window

	check := func(t *testing.T, wantIncome, wantSpend string) {
		t.Helper()
		sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
		if err != nil {
			t.Fatal(err)
		}
		if !decimal.RequireFromString(sum.Income).Equal(decimal.RequireFromString(wantIncome)) {
			t.Errorf("income = %s, want %s", sum.Income, wantIncome)
		}
		if !decimal.RequireFromString(sum.Spending).Equal(decimal.RequireFromString(wantSpend)) {
			t.Errorf("spending = %s, want %s", sum.Spending, wantSpend)
		}
		merchants, err := dash.TopMerchants(ctx, userID, d(1), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range merchants {
			if m.Merchant == "Transfer to Savings" && wantSpend == "1582.15" {
				t.Errorf("proposed transfer leaked into top merchants: %+v", m)
			}
		}
	}

	t.Run("proposed pair excluded", func(t *testing.T) { check(t, "3000.00", "1582.15") })

	t.Run("confirmed pair stays excluded", func(t *testing.T) {
		svc := service.NewTransferMatchingService(repository.NewTransactionRepository(g))
		props, err := svc.ProposeMatches(ctx, userID)
		if err != nil || len(props) != 1 {
			t.Fatalf("proposals = %d, err %v; want 1", len(props), err)
		}
		if _, _, err := svc.Confirm(ctx, userID, props[0].A.ID, props[0].B.ID); err != nil {
			t.Fatal(err)
		}
		check(t, "3000.00", "1582.15")
	})

	t.Run("lone unmatched row still counts", func(t *testing.T) {
		seed(checkingID, 20, "-40.00", "Solo")
		sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
		if err != nil {
			t.Fatal(err)
		}
		if !decimal.RequireFromString(sum.Spending).Equal(decimal.RequireFromString("1622.15")) {
			t.Errorf("spending = %s, want 1622.15", sum.Spending)
		}
	})
}

// TestSpending_DismissedProposedTransferCounts is the #510 regression: a
// coincidental opposite-amount pair (a card purchase and an unrelated
// deposit) is proposed as a transfer and so left out of income, spending and
// budget actuals. Once the user says "not a transfer", both rows count again
// by their exact amounts, the pair is never re-proposed (including after a
// resync rewrites a row), and another user can't dismiss it.
func TestSpending_DismissedProposedTransferCounts(t *testing.T) {
	dash, userID, checkingID, g := newDashboardSvc(t)
	ctx := context.Background()
	suffix := time.Now().Format("150405.000000")

	card := &model.Account{
		UserID: userID, Name: "Card-" + suffix,
		InstitutionSlug: "fixture", AccountType: "credit_card", Currency: "USD",
	}
	if err := g.Create(card).Error; err != nil {
		t.Fatalf("seed card: %v", err)
	}
	dining := &model.Category{Name: "DismissDining", Slug: "dismiss-dining-" + suffix}
	if err := g.Create(dining).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Budget{})
		g.Unscoped().Where("account_id = ?", card.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, card.ID)
		g.Unscoped().Delete(&model.Category{}, dining.ID)
	})

	d := func(day int) time.Time { return time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC) }
	seed := func(acct int64, day int, amt, desc string, cat *int64) *model.Transaction {
		tx := &model.Transaction{
			UserID: userID, AccountID: acct, Amount: decimal.RequireFromString(amt),
			Description: &desc, TransactionDate: d(day), Source: "manual", CategoryID: cat,
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
		return tx
	}
	seed(checkingID, 1, "3000.00", "Payroll", nil)
	cafe := seed(card.ID, 9, "-50.00", "Corner Cafe", &dining.ID)
	venmo := seed(checkingID, 11, "50.00", "Venmo from Sam", nil)

	budgets := service.NewBudgetService(
		repository.NewBudgetRepository(g),
		repository.NewCategoryRepository(g),
	).WithNow(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	budget, err := budgets.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
		CategoryID: dining.ID, Period: "monthly", Amount: decimal.NewFromInt(200),
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}

	xfers := service.NewTransferMatchingService(repository.NewTransactionRepository(g))
	check := func(t *testing.T, wantIncome, wantSpend, wantBudget string, wantProposals int) {
		t.Helper()
		sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
		if err != nil {
			t.Fatal(err)
		}
		if !decimal.RequireFromString(sum.Income).Equal(decimal.RequireFromString(wantIncome)) {
			t.Errorf("income = %s, want %s", sum.Income, wantIncome)
		}
		if !decimal.RequireFromString(sum.Spending).Equal(decimal.RequireFromString(wantSpend)) {
			t.Errorf("spending = %s, want %s", sum.Spending, wantSpend)
		}
		view, err := budgets.Spend(ctx, userID, budget.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !view.Spent.Equal(decimal.RequireFromString(wantBudget)) {
			t.Errorf("budget spent = %s, want %s", view.Spent, wantBudget)
		}
		props, err := xfers.ProposeMatches(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if len(props) != wantProposals {
			t.Errorf("proposals = %d, want %d", len(props), wantProposals)
		}
	}

	t.Run("proposed pair excluded before deciding", func(t *testing.T) {
		check(t, "3000.00", "0", "0", 1)
	})

	t.Run("another user cannot dismiss", func(t *testing.T) {
		other := seedTestUser(t, g)
		if err := xfers.Dismiss(ctx, other, cafe.ID, venmo.ID); !errors.Is(err, service.ErrTransactionNotFound) {
			t.Fatalf("foreign dismiss err = %v, want ErrTransactionNotFound", err)
		}
		if err := xfers.Dismiss(ctx, userID, cafe.ID, 1<<60); !errors.Is(err, service.ErrTransactionNotFound) {
			t.Fatalf("missing-id dismiss err = %v, want ErrTransactionNotFound", err)
		}
		check(t, "3000.00", "0", "0", 1)
	})

	t.Run("dismissed pair counts by exact amounts", func(t *testing.T) {
		// Reversed argument order on purpose: the pair is unordered.
		if err := xfers.Dismiss(ctx, userID, venmo.ID, cafe.ID); err != nil {
			t.Fatal(err)
		}
		check(t, "3050.00", "50.00", "50.00", 0)
	})

	t.Run("dismiss is idempotent", func(t *testing.T) {
		if err := xfers.Dismiss(ctx, userID, cafe.ID, venmo.ID); err != nil {
			t.Fatal(err)
		}
		check(t, "3050.00", "50.00", "50.00", 0)
	})

	t.Run("resync rewriting a row keeps the dismissal", func(t *testing.T) {
		if err := g.Model(&model.Transaction{}).Where("id = ?", venmo.ID).
			Updates(map[string]any{"description": "VENMO CASHOUT", "transaction_date": d(10)}).Error; err != nil {
			t.Fatal(err)
		}
		check(t, "3050.00", "50.00", "50.00", 0)
	})
}
