package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestSpending_HiddenTransactionExcludedAndReversible covers TX-02 / #465: a
// hidden flow row drops out of income, spending and top merchants but stays a
// live ledger row, and unhiding restores the original totals.
func TestSpending_HiddenTransactionExcludedAndReversible(t *testing.T) {
	dash, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	d := func(day int) time.Time { return time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC) }
	seed := func(day int, amt, desc string) *model.Transaction {
		tx := &model.Transaction{
			UserID: userID, AccountID: accountID,
			Amount: decimal.RequireFromString(amt), Description: &desc,
			TransactionDate: d(day), Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
		return tx
	}
	seed(1, "3000.00", "Payroll")
	seed(3, "-82.15", "Whole Foods")
	oneOff := seed(5, "-400.00", "Reimbursed trip")
	reimb := seed(6, "400.00", "Trip reimbursement")

	check := func(label, wantIncome, wantSpend string, wantMerchant bool) {
		t.Helper()
		sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
		if err != nil {
			t.Fatal(err)
		}
		if !decimal.RequireFromString(sum.Income).Equal(decimal.RequireFromString(wantIncome)) {
			t.Errorf("%s: income = %s, want %s", label, sum.Income, wantIncome)
		}
		if !decimal.RequireFromString(sum.Spending).Equal(decimal.RequireFromString(wantSpend)) {
			t.Errorf("%s: spending = %s, want %s", label, sum.Spending, wantSpend)
		}
		ms, err := dash.TopMerchants(ctx, userID, d(1), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 10)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range ms {
			if m.Merchant == "Reimbursed trip" {
				found = true
			}
		}
		if found != wantMerchant {
			t.Errorf("%s: trip in top merchants = %v, want %v", label, found, wantMerchant)
		}
	}
	check("before hide", "3400.00", "482.15", true)

	now := time.Now().UTC()
	for _, id := range []int64{oneOff.ID, reimb.ID} {
		if err := g.Model(&model.Transaction{}).Where("id = ?", id).Update("hidden_at", now).Error; err != nil {
			t.Fatal(err)
		}
	}
	check("hidden", "3000.00", "82.15", false)

	var live int64
	g.Model(&model.Transaction{}).Where("account_id = ?", accountID).Count(&live)
	if live != 4 {
		t.Errorf("hidden rows must stay in the ledger: live rows = %d, want 4", live)
	}

	if err := g.Model(&model.Transaction{}).Where("id IN ?", []int64{oneOff.ID, reimb.ID}).Update("hidden_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	check("unhidden", "3400.00", "482.15", true)
}
