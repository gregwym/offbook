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
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// seedTradeFixture provisions a user, brokerage account, and AAPL asset
// for trade-service tests. Returns the trade service wired against the
// real DB, plus the ids the tests need.
func seedTradeFixture(t *testing.T) (svc *service.TradeService, userID, accountID, aaplID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)
	usdID := testutil.LookupUSDAssetID(t, g)

	displayName := "Apple"
	aapl := &model.Asset{
		Symbol:               "AAPL-" + time.Now().Format("150405.000000000"),
		Kind:                 model.AssetKindEquity,
		DisplayName:          &displayName,
		Precision:            4,
		QuoteCurrencyAssetID: &usdID,
	}
	if err := g.Create(aapl).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, aapl.ID) })

	acct := &model.Account{
		UserID: userID, Name: "Brokerage-" + time.Now().Format("150405.000000000"),
		InstitutionSlug: "fixture", AccountType: "investment", Currency: "USD",
		PrimaryQuoteAssetID: usdID,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}

	svc = service.NewTradeService(
		g,
		repository.NewAccountRepository(g),
		repository.NewAssetRepository(g),
		repository.NewTransactionRepository(g),
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewUserRepository(g),
	)
	return svc, userID, acct.ID, aapl.ID, g
}

func TestTradeService_Record_Buy_WritesPairAndPositions(t *testing.T) {
	svc, userID, accountID, aapl, g := seedTradeFixture(t)
	ctx := context.Background()

	rec, err := svc.Record(ctx, userID, accountID, service.RecordTradeInput{
		Kind:      "buy",
		AssetID:   aapl,
		Quantity:  decimal.NewFromInt(10),
		Price:     decimal.NewFromInt(150),
		TradeDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	if rec.SecurityLeg.TransferPairID == nil || rec.CashLeg.TransferPairID == nil {
		t.Fatal("legs missing transfer_pair_id")
	}
	if *rec.SecurityLeg.TransferPairID != rec.CashLeg.ID || *rec.CashLeg.TransferPairID != rec.SecurityLeg.ID {
		t.Errorf("legs not cross-linked: sec.pair=%v cash.pair=%v sec.id=%d cash.id=%d",
			rec.SecurityLeg.TransferPairID, rec.CashLeg.TransferPairID, rec.SecurityLeg.ID, rec.CashLeg.ID)
	}

	if !rec.SecurityLeg.Amount.Equal(decimal.NewFromInt(10)) {
		t.Errorf("security amount = %s, want +10", rec.SecurityLeg.Amount)
	}
	if !rec.CashLeg.Amount.Equal(decimal.NewFromInt(-1500)) {
		t.Errorf("cash amount = %s, want -1500", rec.CashLeg.Amount)
	}
	// Both legs are classified trade_leg (ADR-0017) — excluded from flow
	// analytics, included in quantity reconstruction.
	if rec.SecurityLeg.Kind != model.KindTradeLeg || rec.CashLeg.Kind != model.KindTradeLeg {
		t.Errorf("leg kinds = (%q, %q), want both %q", rec.SecurityLeg.Kind, rec.CashLeg.Kind, model.KindTradeLeg)
	}
	if rec.SecurityPosition == nil || !rec.SecurityPosition.Quantity.Equal(decimal.NewFromInt(10)) {
		t.Errorf("security position qty = %v, want 10", rec.SecurityPosition)
	}
	if rec.SecurityPosition.CostBasis == nil || !rec.SecurityPosition.CostBasis.Equal(decimal.NewFromInt(1500)) {
		t.Errorf("security cost basis = %v, want 1500", rec.SecurityPosition.CostBasis)
	}

	// Two transactions written, one per leg.
	var n int64
	g.Model(&model.Transaction{}).
		Where("user_id = ? AND account_id = ?", userID, accountID).Count(&n)
	if n != 2 {
		t.Errorf("got %d transactions, want 2", n)
	}
}

// TestTradeService_Record_WritesTradePriceForValuation is the #352
// regression: a manually recorded equity trade must write its user-entered
// price as a Tier-1 `prices` row (source='trade') so the position values at
// last-trade price instead of being permanently $0/partial (there is no
// equity price provider — ADR-0014 ships only crypto + FX).
func TestTradeService_Record_WritesTradePriceForValuation(t *testing.T) {
	svc, userID, accountID, aapl, g := seedTradeFixture(t)
	ctx := context.Background()
	usdID := testutil.LookupUSDAssetID(t, g)
	tradeDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	rec, err := svc.Record(ctx, userID, accountID, service.RecordTradeInput{
		Kind: "buy", AssetID: aapl,
		Quantity: decimal.NewFromInt(10), Price: decimal.NewFromInt(150),
		TradeDate: tradeDate,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	// A Tier-1 price observation is written for the security quoted in the
	// account's cash sleeve.
	var p model.Price
	if err := g.Where("asset_id = ? AND quote_asset_id = ? AND source = ?", aapl, usdID, "trade").
		First(&p).Error; err != nil {
		t.Fatalf("expected a source='trade' price row: %v", err)
	}
	if !p.Price.Equal(decimal.NewFromInt(150)) {
		t.Errorf("trade price = %s, want 150", p.Price)
	}
	if !p.AsOf.Equal(tradeDate) {
		t.Errorf("trade price as_of = %s, want %s", p.AsOf, tradeDate)
	}

	// Valuation now prices the equity position fresh at the trade price —
	// never silently $0 for a fully-described manual holding.
	val := valuation.NewService(
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewAssetRepository(g),
		repository.NewAccountRepository(g),
	)
	got, err := val.Value(ctx, *rec.SecurityPosition, tradeDate, usdID)
	if err != nil {
		t.Fatalf("value security position: %v", err)
	}
	if !got.Equal(decimal.NewFromInt(1500)) {
		t.Errorf("security value = %s, want 1500", got)
	}
}

func TestTradeService_Record_RejectsNonBrokerageAccount(t *testing.T) {
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	usdID := testutil.LookupUSDAssetID(t, g)
	displayName := "Apple"
	aapl := &model.Asset{
		Symbol: "AAPL-nb-" + time.Now().Format("150405.000000000"),
		Kind:   model.AssetKindEquity, DisplayName: &displayName, Precision: 4,
		QuoteCurrencyAssetID: &usdID,
	}
	if err := g.Create(aapl).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, aapl.ID) })

	acct := &model.Account{
		UserID: userID, Name: "Checking-" + time.Now().Format("150405.000000000"),
		InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD",
		PrimaryQuoteAssetID: usdID,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	svc := service.NewTradeService(g,
		repository.NewAccountRepository(g),
		repository.NewAssetRepository(g),
		repository.NewTransactionRepository(g),
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewUserRepository(g),
	)
	_, err := svc.Record(context.Background(), userID, acct.ID, service.RecordTradeInput{
		Kind: "buy", AssetID: aapl.ID,
		Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(150),
		TradeDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, service.ErrUnsupportedAccount) {
		t.Fatalf("err = %v, want ErrUnsupportedAccount", err)
	}
}

func TestTradeService_Record_SellExceedsHoldings(t *testing.T) {
	svc, userID, accountID, aapl, _ := seedTradeFixture(t)
	ctx := context.Background()
	// Buy 5.
	if _, err := svc.Record(ctx, userID, accountID, service.RecordTradeInput{
		Kind: "buy", AssetID: aapl,
		Quantity: decimal.NewFromInt(5), Price: decimal.NewFromInt(100),
		TradeDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("buy: %v", err)
	}
	// Sell 10 — too many.
	_, err := svc.Record(ctx, userID, accountID, service.RecordTradeInput{
		Kind: "sell", AssetID: aapl,
		Quantity: decimal.NewFromInt(10), Price: decimal.NewFromInt(120),
		TradeDate: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, service.ErrSellExceedsHoldings) {
		t.Fatalf("err = %v, want ErrSellExceedsHoldings", err)
	}
}

func TestTradeService_Record_TenantIsolation(t *testing.T) {
	svc, userA, accountA, aapl, g := seedTradeFixture(t)
	userB := seedTestUser(t, g)
	ctx := context.Background()

	// User B should not be able to write into user A's account.
	_, err := svc.Record(ctx, userB, accountA, service.RecordTradeInput{
		Kind: "buy", AssetID: aapl,
		Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(100),
		TradeDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, service.ErrAccountNotFound) {
		t.Fatalf("err = %v, want ErrAccountNotFound (cross-tenant attempt)", err)
	}
	_ = userA
}

// TestTradeService_TradeLegsExcludedFromSpending: a buy's cash leg is
// kind=trade_leg, so it must not count as spending in flow analytics (#293
// slice 3 — the flow filter is kind='flow'). Previously trade legs leaked
// into spending because they set transfer_pair_id but not is_transfer.
func TestTradeService_TradeLegsExcludedFromSpending(t *testing.T) {
	svc, userID, accountID, aapl, g := seedTradeFixture(t)
	ctx := context.Background()

	if _, err := svc.Record(ctx, userID, accountID, service.RecordTradeInput{
		Kind: "buy", AssetID: aapl,
		Quantity: decimal.NewFromInt(10), Price: decimal.NewFromInt(150),
		TradeDate: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	dash := repository.NewDashboardRepository(g)
	agg, err := dash.Summarize(ctx, userID, time.Now().AddDate(0, -1, 0), time.Now().AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	// The -1500 cash leg is a trade_leg, not a flow → excluded from spending.
	if !agg.Spending.IsZero() {
		t.Errorf("spending = %s, want 0 (trade legs excluded from flow analytics)", agg.Spending)
	}
}
