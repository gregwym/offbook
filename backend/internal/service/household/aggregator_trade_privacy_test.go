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

// TestAggregator_TradeLegs_HiddenFromBalanceOnly — issue #238 privacy
// criterion: when an account is shared at `balance_only`, paired trade
// legs (both the security and the cash leg) must NOT leak into
// transaction-touching aggregates (transaction_count, by_category).
// The position-driven net-worth still reflects the trade — that's the
// whole point of balance_only — but per-row visibility stays gated.
func TestAggregator_TradeLegs_HiddenFromBalanceOnly(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "trade-priv-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Trade Privacy", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	acct := seedAccount(t, g, ownerID, "brokerage")
	setBalance(t, g, acct, "10000")
	setShare(t, g, acct.ID, hh.ID, model.VisibilityBalanceOnly)

	// Spawn an AAPL asset and write a paired trade directly via the repo
	// (mirroring what the trade service would do).
	displayName := "Apple"
	aapl := &model.Asset{
		Symbol: "AAPL-priv-" + time.Now().Format("150405.000000000"),
		Kind:   model.AssetKindEquity, DisplayName: &displayName, Precision: 4,
	}
	if err := g.Create(aapl).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, aapl.ID) })

	when := time.Now().Add(-time.Hour)
	secLeg := &model.Transaction{
		UserID: ownerID, AccountID: acct.ID, AssetID: aapl.ID,
		Amount: decimal.NewFromInt(10), TransactionDate: when, Source: "manual",
	}
	cashLeg := &model.Transaction{
		UserID: ownerID, AccountID: acct.ID, AssetID: acct.PrimaryQuoteAssetID,
		Amount: decimal.NewFromInt(-1500), TransactionDate: when, Source: "manual",
	}
	repo := repository.NewTransactionRepository(g)
	if err := repo.CreateTradePair(ctx, secLeg, cashLeg); err != nil {
		t.Fatalf("create trade pair: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.Transaction{}, secLeg.ID)
		g.Unscoped().Delete(&model.Transaction{}, cashLeg.ID)
	})

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if d.TransactionCount != 0 {
		t.Errorf("TransactionCount = %d, want 0 (trade legs must not leak from balance_only)", d.TransactionCount)
	}
	for _, row := range d.ByCategory {
		if row.Amount == "1500" || row.Amount == "-1500" {
			t.Errorf("trade cash leg leaked into ByCategory: %+v", row)
		}
	}
	// Net worth still reflects the account balance — balance_only's contract.
	if d.NetWorth != "10000" {
		t.Errorf("NetWorth = %q, want 10000 (balance_only still surfaces totals)", d.NetWorth)
	}
}

// TestAggregator_TradeLegs_VisibleWithFullVisibility — counterpart to
// the test above: with `balance_and_txns`, trade legs DO contribute to
// transaction_count (per-tenant visibility is fully consented).
func TestAggregator_TradeLegs_VisibleWithFullVisibility(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "trade-vis-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Trade Visible", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	acct := seedAccount(t, g, ownerID, "brokerage")
	setBalance(t, g, acct, "10000")
	setShare(t, g, acct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	displayName := "Apple"
	aapl := &model.Asset{
		Symbol: "AAPL-vis-" + time.Now().Format("150405.000000000"),
		Kind:   model.AssetKindEquity, DisplayName: &displayName, Precision: 4,
	}
	if err := g.Create(aapl).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, aapl.ID) })

	when := time.Now().Add(-time.Hour)
	secLeg := &model.Transaction{
		UserID: ownerID, AccountID: acct.ID, AssetID: aapl.ID,
		Amount: decimal.NewFromInt(10), TransactionDate: when, Source: "manual",
	}
	cashLeg := &model.Transaction{
		UserID: ownerID, AccountID: acct.ID, AssetID: acct.PrimaryQuoteAssetID,
		Amount: decimal.NewFromInt(-1500), TransactionDate: when, Source: "manual",
	}
	if err := repository.NewTransactionRepository(g).CreateTradePair(ctx, secLeg, cashLeg); err != nil {
		t.Fatalf("create trade pair: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.Transaction{}, secLeg.ID)
		g.Unscoped().Delete(&model.Transaction{}, cashLeg.ID)
	})

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if d.TransactionCount != 2 {
		t.Errorf("TransactionCount = %d, want 2 (both trade legs visible)", d.TransactionCount)
	}
}
