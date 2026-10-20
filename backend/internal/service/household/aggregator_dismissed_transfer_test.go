package household_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// TestAggregator_DismissedTransferProposal_Counts — a coincidental
// opposite-amount pair across two shared accounts is a proposed transfer and
// stays out of household spending and income; once the owner says "not a
// transfer" (#510) both rows count in the household aggregate too.
func TestAggregator_DismissedTransferProposal_Counts(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "dismiss-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Dismissed Transfer", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	card := seedAccount(t, g, ownerID, "dismiss-card")
	checking := seedAccount(t, g, ownerID, "dismiss-checking")
	for _, a := range []*model.Account{card, checking} {
		setBalance(t, g, a, "500")
		setShare(t, g, a.ID, hh.ID, model.VisibilityBalanceAndTxns)
	}

	var cat model.Category
	if err := g.Order("id").First(&cat).Error; err != nil {
		t.Fatalf("seeded category: %v", err)
	}
	when := time.Now().Add(-time.Hour)
	seed := func(acct *model.Account, amt string, catID *int64) *model.Transaction {
		tx := &model.Transaction{
			UserID: ownerID, AccountID: acct.ID, Amount: decimal.RequireFromString(amt),
			CategoryID: catID, Source: "manual", TransactionDate: when,
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed tx: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
		return tx
	}
	purchase := seed(card, "-50", &cat.ID)
	deposit := seed(checking, "50", nil)

	totals := func() (string, string) {
		d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
		if err != nil {
			t.Fatalf("Dashboard: %v", err)
		}
		return d.Spending, d.Income
	}
	if s, i := totals(); s != "0" || i != "0" {
		t.Fatalf("proposed: Spending=%q Income=%q, want 0 and 0", s, i)
	}
	if err := repository.NewTransactionRepository(g).DismissTransferPair(ctx, ownerID, purchase.ID, deposit.ID); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if s, i := totals(); s != "50" || i != "50" {
		t.Errorf("dismissed: Spending=%q Income=%q, want 50 and 50", s, i)
	}
}
