package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestSpending_TransferCategoryExcludedWithoutPair covers TX-04 / Goal #423
// gate 3: a credit-card payment to a card Offbook does not track (or a Plaid
// row mapped to the system "transfer" category) has no counterpart row to
// pair with, so is_transfer stays false. The Transfer category itself is the
// user's/Plaid's statement that the money only moved, so it must not count
// as spending, income, a category slice or a top merchant.
func TestSpending_TransferCategoryExcludedWithoutPair(t *testing.T) {
	dash, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	var transferCat model.Category
	if err := g.Where("slug = ?", "transfer").First(&transferCat).Error; err != nil {
		t.Fatalf("system transfer category: %v", err)
	}

	d := func(day int) time.Time { return time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC) }
	seed := func(day int, amt, desc string, cat *int64) {
		tx := &model.Transaction{
			UserID: userID, AccountID: accountID, CategoryID: cat,
			Amount: decimal.RequireFromString(amt), Description: &desc,
			TransactionDate: d(day), Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	seed(1, "3000.00", "Payroll", nil)
	seed(3, "-82.15", "Whole Foods", nil)
	seed(10, "-700.00", "Card payment", &transferCat.ID)
	seed(12, "300.00", "Transfer in", &transferCat.ID)

	sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(sum.Income).Equal(decimal.RequireFromString("3000.00")) {
		t.Errorf("income = %s, want 3000.00", sum.Income)
	}
	if !decimal.RequireFromString(sum.Spending).Equal(decimal.RequireFromString("82.15")) {
		t.Errorf("spending = %s, want 82.15", sum.Spending)
	}
	merchants, err := dash.TopMerchants(ctx, userID, d(1), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range merchants {
		if m.Merchant == "Card payment" {
			t.Errorf("transfer-category row leaked into top merchants: %+v", m)
		}
	}
}
