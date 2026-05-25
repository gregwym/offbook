package plaid_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// fakePlaidClient implements plaidsvc.Client with hand-set return
// values, so reconciliation tests can drive specific snapshots without
// standing up an httptest server.
type fakePlaidClient struct {
	holdings plaidsvc.HoldingsResult
	invTxns  plaidsvc.InvestmentTransactionsResult
}

func (f *fakePlaidClient) CreateLinkToken(context.Context, int64) (plaidsvc.LinkToken, error) {
	return plaidsvc.LinkToken{}, fmt.Errorf("not used in test")
}
func (f *fakePlaidClient) ExchangePublicToken(context.Context, string) (plaidsvc.Item, error) {
	return plaidsvc.Item{}, fmt.Errorf("not used in test")
}
func (f *fakePlaidClient) FetchAccounts(context.Context, string) (plaidsvc.AccountsResult, error) {
	return plaidsvc.AccountsResult{}, fmt.Errorf("not used in test")
}
func (f *fakePlaidClient) SyncTransactions(context.Context, string, string) (plaidsvc.SyncTransactionsPage, error) {
	return plaidsvc.SyncTransactionsPage{}, fmt.Errorf("not used in test")
}
func (f *fakePlaidClient) FetchInvestmentTransactions(context.Context, string, time.Time, time.Time) (plaidsvc.InvestmentTransactionsResult, error) {
	return f.invTxns, nil
}
func (f *fakePlaidClient) FetchHoldings(context.Context, string) (plaidsvc.HoldingsResult, error) {
	return f.holdings, nil
}

// seedHoldingsFixture provisions a user + brokerage account + AAPL
// asset + a starting AAPL position of 10 shares, and returns the
// plaid item so the test can call SyncHoldings.
func seedHoldingsFixture(t *testing.T, g *gorm.DB, snapshotQty decimal.Decimal) (svc *plaidsvc.Service, userID int64, accountID int64, aaplID int64, item *model.PlaidItem) {
	t.Helper()
	userID = seedPlaidTestUser(t, g)
	usdID := testutil.LookupUSDAssetID(t, g)

	displayName := "Apple Inc"
	aapl := &model.Asset{
		Symbol: "AAPL-recon-" + time.Now().Format("150405.000000000"),
		Kind:   model.AssetKindEquity, DisplayName: &displayName, Precision: 4,
	}
	if err := g.Create(aapl).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, aapl.ID) })
	aaplID = aapl.ID

	plaidAcct := "pacct-recon-" + time.Now().Format("150405.000000000")
	acct := &model.Account{
		UserID: userID, Name: "BrokerageRecon", InstitutionSlug: "fixture",
		AccountType: "investment", Currency: "USD", PrimaryQuoteAssetID: usdID,
		PlaidAccountID: &plaidAcct,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	accountID = acct.ID

	// Starting position: 10 shares of AAPL. The reconciliation test
	// will assert this gets adjusted to match Plaid's snapshot.
	if err := g.Exec(`
		INSERT INTO positions (user_id, account_id, asset_id, quantity, updated_at)
		VALUES (?, ?, ?, ?, NOW())
		ON CONFLICT (account_id, asset_id) WHERE deleted_at IS NULL
		DO UPDATE SET quantity = EXCLUDED.quantity, updated_at = NOW()
	`, userID, accountID, aaplID, decimal.NewFromInt(10)).Error; err != nil {
		t.Fatalf("seed position: %v", err)
	}

	// Build the fake Plaid client with the supplied snapshot quantity.
	plaidItemID := "pitem-recon-" + time.Now().Format("150405.000000000")
	plaidSecID := "psec-recon-" + time.Now().Format("150405.000000000")
	fake := &fakePlaidClient{
		holdings: plaidsvc.HoldingsResult{
			Holdings: []plaidsvc.PlaidHolding{
				{
					PlaidAccountID:   plaidAcct,
					PlaidSecurityID:  plaidSecID,
					Quantity:         snapshotQty,
					InstitutionPrice: decimal.NewFromInt(195),
					InstitutionValue: snapshotQty.Mul(decimal.NewFromInt(195)),
					CostBasis:        decimal.Zero,
					IsoCurrencyCode:  "USD",
				},
			},
			Securities: []plaidsvc.PlaidSecurity{
				{
					PlaidSecurityID: plaidSecID,
					TickerSymbol:    aapl.Symbol,
					Name:            "Apple Inc",
					Type:            "equity",
					IsoCurrencyCode: "USD",
				},
			},
		},
	}

	// Persist a Plaid item so the service can find the access token.
	box, err := crypto.NewSecretBox(make([]byte, 32))
	if err != nil {
		t.Fatalf("secretbox: %v", err)
	}
	enc, err := box.Encrypt([]byte("access-token-recon"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	item = &model.PlaidItem{
		UserID:         userID,
		PlaidItemID:    plaidItemID,
		AccessTokenEnc: enc,
		Status:         "active",
	}
	if err := g.Create(item).Error; err != nil {
		t.Fatalf("seed item: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.PlaidItem{}, item.ID) })

	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), nil)
	svc = plaidsvc.NewService(
		fake, box,
		repository.NewPlaidItemRepository(g),
		repository.NewAccountRepository(g),
		repository.NewTransactionRepository(g),
		repository.NewPlaidSyncErrorRepository(g),
		repository.NewAssetRepository(g),
		repository.NewPositionRepository(g),
		piiSvc,
		nil, // catMapper unused
		g,
	)
	return svc, userID, accountID, aaplID, item
}

// TestSyncHoldings_AdjustsPositionWithoutSyntheticTxns covers the
// ADR-0013 §3 invariant: when Plaid's snapshot disagrees with our
// derived position, we update the position to match Plaid but
// DO NOT insert any synthetic trade rows to bridge the gap.
func TestSyncHoldings_AdjustsPositionWithoutSyntheticTxns(t *testing.T) {
	g := openPlaidTestDB(t)
	svc, userID, accountID, aaplID, item := seedHoldingsFixture(t, g, decimal.NewFromInt(8))
	ctx := context.Background()

	// Before: 0 transactions for this user on this account.
	var beforeCount int64
	g.Model(&model.Transaction{}).Where("user_id = ?", userID).Count(&beforeCount)
	if beforeCount != 0 {
		t.Fatalf("fixture leak: %d transactions before sync, want 0", beforeCount)
	}

	res, err := svc.SyncHoldings(ctx, userID, item.PlaidItemID)
	if err != nil {
		t.Fatalf("SyncHoldings: %v", err)
	}
	if res.Holdings != 1 {
		t.Errorf("Holdings = %d, want 1", res.Holdings)
	}
	if res.PositionsAdjusted != 1 {
		t.Errorf("PositionsAdjusted = %d, want 1 (10 → 8)", res.PositionsAdjusted)
	}
	if res.PricesObserved != 1 {
		t.Errorf("PricesObserved = %d, want 1", res.PricesObserved)
	}

	// Position should now reflect Plaid's snapshot.
	var pos model.Position
	if err := g.Where("user_id = ? AND account_id = ? AND asset_id = ?", userID, accountID, aaplID).
		First(&pos).Error; err != nil {
		t.Fatalf("load position: %v", err)
	}
	if !pos.Quantity.Equal(decimal.NewFromInt(8)) {
		t.Errorf("quantity = %s, want 8 (Plaid snapshot wins)", pos.Quantity)
	}

	// Critical invariant: NO synthetic transactions inserted.
	var afterCount int64
	g.Model(&model.Transaction{}).Where("user_id = ?", userID).Count(&afterCount)
	if afterCount != 0 {
		t.Errorf("synthesized %d transactions during reconciliation — invariant violation", afterCount)
	}

	// A price observation was written.
	var priceCount int64
	g.Model(&model.Price{}).Where("asset_id = ?", aaplID).Count(&priceCount)
	if priceCount != 1 {
		t.Errorf("price observations for asset %d = %d, want 1", aaplID, priceCount)
	}
}
