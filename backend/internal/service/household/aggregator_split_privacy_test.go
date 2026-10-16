package household_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// seedSplitTxn creates a flow transaction and allocates it across the two
// given categories (#438).
func seedSplitTxn(t *testing.T, g *gorm.DB, userID, accountID int64, total string, catA, catB int64, amtA, amtB string, when time.Time) {
	t.Helper()
	amt, _ := decimal.NewFromString(total)
	tx := &model.Transaction{
		UserID: userID, AccountID: accountID, Amount: amt,
		Source: "manual", TransactionDate: when,
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed split parent: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
	for _, l := range []struct {
		cat int64
		amt string
	}{{catA, amtA}, {catB, amtB}} {
		a, _ := decimal.NewFromString(l.amt)
		cat := l.cat
		s := &model.TransactionSplit{TransactionID: tx.ID, UserID: userID, CategoryID: &cat, Amount: a}
		if err := g.Create(s).Error; err != nil {
			t.Fatalf("seed split line: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.TransactionSplit{}, s.ID) })
	}
}

// TestAggregator_SplitLines_PrivacyAndSingleCount — split lines obey the same
// visibility rules as their parent: a split on a balance_only account never
// reaches by_category, and a split on a balance_and_txns account is counted
// once per category (lines sum to the parent, no double count).
func TestAggregator_SplitLines_PrivacyAndSingleCount(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "split-priv-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Split Privacy", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	balOnly := seedAccount(t, g, ownerID, "split-bal-only")
	full := seedAccount(t, g, ownerID, "split-full")
	setBalance(t, g, balOnly, "1000")
	setBalance(t, g, full, "500")
	setShare(t, g, balOnly.ID, hh.ID, model.VisibilityBalanceOnly)
	setShare(t, g, full.ID, hh.ID, model.VisibilityBalanceAndTxns)

	var cats []model.Category
	if err := g.Order("id").Limit(2).Find(&cats).Error; err != nil || len(cats) < 2 {
		t.Fatalf("need 2 seeded categories: %v (got %d)", err, len(cats))
	}

	when := time.Now().Add(-time.Hour)
	seedSplitTxn(t, g, ownerID, balOnly.ID, "-100", cats[0].ID, cats[1].ID, "-70", "-30", when)
	seedSplitTxn(t, g, ownerID, full.ID, "-50", cats[0].ID, cats[1].ID, "-20", "-30", when)

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if d.Spending != "50" {
		t.Errorf("Spending = %q, want 50 (balance_only split excluded, shared split counted once)", d.Spending)
	}
	sum := decimal.Zero
	for _, row := range d.ByCategory {
		a, _ := decimal.NewFromString(row.Amount)
		sum = sum.Add(a)
		if row.Amount == "-70" || row.Amount == "70" {
			t.Errorf("balance_only split line leaked into ByCategory: %+v", row)
		}
	}
	if !sum.Abs().Equal(decimal.NewFromInt(50)) {
		t.Errorf("ByCategory total = %s, want |50| (lines sum to parent once)", sum)
	}
}
