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
