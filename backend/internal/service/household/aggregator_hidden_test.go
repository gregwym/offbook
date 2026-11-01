package household_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// TestAggregator_HiddenTransaction_ExcludedFromSpending — a hidden flow
// transaction on a shared account drops out of household spending and
// by_category (TX-02, #465) and returns when unhidden.
func TestAggregator_HiddenTransaction_ExcludedFromSpending(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "hidden-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Hidden Tx", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	acct := seedAccount(t, g, ownerID, "hidden-full")
	setBalance(t, g, acct, "500")
	setShare(t, g, acct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	var cat model.Category
	if err := g.Order("id").First(&cat).Error; err != nil {
		t.Fatalf("seeded category: %v", err)
	}
	amt, _ := decimal.NewFromString("-40")
	tx := &model.Transaction{
		UserID: ownerID, AccountID: acct.ID, Amount: amt, CategoryID: &cat.ID,
		Source: "manual", TransactionDate: seedWhen(),
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed tx: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })

	spend := func() (string, int) {
		d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
		if err != nil {
			t.Fatalf("Dashboard: %v", err)
		}
		return d.Spending, len(d.ByCategory)
	}
	if s, _ := spend(); s != "40" {
		t.Fatalf("visible Spending = %q, want 40", s)
	}
	now := time.Now()
	if err := g.Model(&model.Transaction{}).Where("id = ?", tx.ID).Update("hidden_at", now).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	if s, n := spend(); s != "0" || n != 0 {
		t.Errorf("hidden: Spending=%q categories=%d, want 0 and none", s, n)
	}
	if err := g.Model(&model.Transaction{}).Where("id = ?", tx.ID).Update("hidden_at", nil).Error; err != nil {
		t.Fatalf("unhide: %v", err)
	}
	if s, _ := spend(); s != "40" {
		t.Errorf("unhidden Spending = %q, want 40", s)
	}
}
