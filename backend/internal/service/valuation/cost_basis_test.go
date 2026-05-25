package valuation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// seedTradeAccount creates a user + brokerage account + AAPL asset and
// returns ids needed by the cost-basis tests. Cleanup is registered for
// every row.
func seedTradeAccount(t *testing.T, g *gorm.DB) (userID, accountID, usdID, aaplID int64) {
	t.Helper()
	usdID = testutil.LookupUSDAssetID(t, g)
	u := &model.User{
		Email:                  "cb-" + time.Now().Format("150405.000000000") + "@example.test",
		PasswordHash:           "x",
		LastScope:              model.ScopePersonal,
		DefaultScope:           model.ScopePersonal,
		PrimaryCurrencyAssetID: usdID,
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Transaction{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Position{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Account{})
		g.Unscoped().Delete(&model.User{}, u.ID)
	})

	displayName := "Apple"
	aapl := &model.Asset{Symbol: "AAPL-" + time.Now().Format("150405.000000000"), Kind: model.AssetKindEquity, DisplayName: &displayName, Precision: 4}
	if err := g.Create(aapl).Error; err != nil {
		t.Fatalf("seed AAPL: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, aapl.ID) })

	acct := &model.Account{
		UserID: u.ID, Name: "Brokerage-" + time.Now().Format("150405.000000000"),
		InstitutionSlug: "fixture", AccountType: "investment", Currency: "USD",
		PrimaryQuoteAssetID: usdID,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return u.ID, acct.ID, usdID, aapl.ID
}

// createPair builds a buy or sell trade pair as the trade_service would.
// kind ∈ {"buy","sell"}; qty/price are positive magnitudes. Returns the
// (security_leg, cash_leg) IDs for downstream assertions.
func createPair(t *testing.T, g *gorm.DB, userID, accountID, secAsset, cashAsset int64, kind string, qty, price decimal.Decimal, date time.Time) {
	t.Helper()
	var secAmt, cashAmt decimal.Decimal
	switch kind {
	case "buy":
		secAmt = qty
		cashAmt = price.Mul(qty).Neg()
	case "sell":
		secAmt = qty.Neg()
		cashAmt = price.Mul(qty)
	default:
		t.Fatalf("bad kind %q", kind)
	}
	repo := repository.NewTransactionRepository(g)
	secLeg := &model.Transaction{UserID: userID, AccountID: accountID, AssetID: secAsset, Amount: secAmt, TransactionDate: date, Source: "manual"}
	cashLeg := &model.Transaction{UserID: userID, AccountID: accountID, AssetID: cashAsset, Amount: cashAmt, TransactionDate: date, Source: "manual"}
	if err := repo.CreateTradePair(context.Background(), secLeg, cashLeg); err != nil {
		t.Fatalf("create pair: %v", err)
	}
}

func TestRecompute_BuyOnly(t *testing.T) {
	g := openTestDB(t)
	userID, accountID, usd, aapl := seedTradeAccount(t, g)
	ctx := context.Background()

	// Buy 10 AAPL @ $150 = $1500 cost basis.
	createPair(t, g, userID, accountID, aapl, usd, "buy",
		decimal.NewFromInt(10), decimal.NewFromInt(150),
		time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))

	res, err := valuation.Recompute(ctx,
		repository.NewTransactionRepository(g),
		repository.NewPriceRepository(g),
		userID, accountID, aapl, usd,
	)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if !res.Quantity.Equal(decimal.NewFromInt(10)) {
		t.Errorf("qty = %s, want 10", res.Quantity)
	}
	if !res.CostBasis.Equal(decimal.NewFromInt(1500)) {
		t.Errorf("cost basis = %s, want 1500", res.CostBasis)
	}
	if !res.HasCostBasis {
		t.Error("HasCostBasis = false, want true")
	}
}

func TestRecompute_AverageCost_TwoBuysOneSell(t *testing.T) {
	g := openTestDB(t)
	userID, accountID, usd, aapl := seedTradeAccount(t, g)
	ctx := context.Background()

	// 10 @ $100 + 10 @ $200 = 20 shares, $3000 cost basis.
	createPair(t, g, userID, accountID, aapl, usd, "buy",
		decimal.NewFromInt(10), decimal.NewFromInt(100),
		time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	createPair(t, g, userID, accountID, aapl, usd, "buy",
		decimal.NewFromInt(10), decimal.NewFromInt(200),
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC))
	// Sell 5 → proportional reduction: per-unit = 3000/20 = 150.
	// Removed = 150 * 5 = 750. New cb = 2250 over 15 shares.
	createPair(t, g, userID, accountID, aapl, usd, "sell",
		decimal.NewFromInt(5), decimal.NewFromInt(180),
		time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC))

	res, err := valuation.Recompute(ctx,
		repository.NewTransactionRepository(g),
		repository.NewPriceRepository(g),
		userID, accountID, aapl, usd,
	)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if !res.Quantity.Equal(decimal.NewFromInt(15)) {
		t.Errorf("qty = %s, want 15", res.Quantity)
	}
	if !res.CostBasis.Equal(decimal.NewFromInt(2250)) {
		t.Errorf("cost basis = %s, want 2250", res.CostBasis)
	}
}

func TestRecompute_SellAll_ResetsBasis(t *testing.T) {
	g := openTestDB(t)
	userID, accountID, usd, aapl := seedTradeAccount(t, g)
	ctx := context.Background()

	createPair(t, g, userID, accountID, aapl, usd, "buy",
		decimal.NewFromInt(10), decimal.NewFromInt(100),
		time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	createPair(t, g, userID, accountID, aapl, usd, "sell",
		decimal.NewFromInt(10), decimal.NewFromInt(120),
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC))

	res, err := valuation.Recompute(ctx,
		repository.NewTransactionRepository(g),
		repository.NewPriceRepository(g),
		userID, accountID, aapl, usd,
	)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if !res.Quantity.IsZero() {
		t.Errorf("qty = %s, want 0", res.Quantity)
	}
	if !res.CostBasis.IsZero() {
		t.Errorf("cost basis = %s, want 0 after full liquidation", res.CostBasis)
	}
}

func TestRecompute_FXAcrossPrimary(t *testing.T) {
	g := openTestDB(t)
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")

	// User's primary currency is USD; account holds EUR cash + an EUR-
	// quoted "Bund" position. Cost basis should land in USD via the
	// EUR→USD trade-date price.
	u := &model.User{
		Email:                  "cb-fx-" + time.Now().Format("150405.000000000") + "@example.test",
		PasswordHash:           "x",
		LastScope:              model.ScopePersonal,
		DefaultScope:           model.ScopePersonal,
		PrimaryCurrencyAssetID: usd,
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Transaction{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Account{})
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	bundName := "German Bund"
	bund := &model.Asset{Symbol: "BUND-" + time.Now().Format("150405.000000000"), Kind: model.AssetKindBond, DisplayName: &bundName, Precision: 4, QuoteCurrencyAssetID: &eur}
	if err := g.Create(bund).Error; err != nil {
		t.Fatalf("seed BUND: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, bund.ID) })

	acct := &model.Account{
		UserID: u.ID, Name: "EUR-Brokerage-" + time.Now().Format("150405.000000000"),
		InstitutionSlug: "fixture", AccountType: "investment", Currency: "EUR",
		PrimaryQuoteAssetID: eur,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}

	// Trade-date EUR→USD = 1.10 (i.e. 100 EUR = 110 USD).
	tradeDate := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	if err := repository.NewPriceRepository(g).Insert(context.Background(), &model.Price{
		AssetID:      eur,
		QuoteAssetID: usd,
		AsOf:         tradeDate,
		Price:        decimal.NewFromFloat(1.10),
		Source:       "manual",
	}); err != nil {
		t.Fatalf("seed price: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("asset_id = ? AND quote_asset_id = ?", eur, usd).Delete(&model.Price{})
	})

	// Buy 10 BUND @ 100 EUR = 1000 EUR cash out = 1100 USD cost basis.
	createPair(t, g, u.ID, acct.ID, bund.ID, eur, "buy",
		decimal.NewFromInt(10), decimal.NewFromInt(100), tradeDate)

	res, err := valuation.Recompute(context.Background(),
		repository.NewTransactionRepository(g),
		repository.NewPriceRepository(g),
		u.ID, acct.ID, bund.ID, usd,
	)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if !res.Quantity.Equal(decimal.NewFromInt(10)) {
		t.Errorf("qty = %s, want 10", res.Quantity)
	}
	if !res.CostBasis.Equal(decimal.NewFromInt(1100)) {
		t.Errorf("cost basis = %s, want 1100 USD", res.CostBasis)
	}
}

func TestRecompute_FXUnavailable_ReturnsErr(t *testing.T) {
	g := openTestDB(t)
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")

	u := &model.User{
		Email:        "cb-nofx-" + time.Now().Format("150405.000000000") + "@example.test",
		PasswordHash: "x", LastScope: model.ScopePersonal, DefaultScope: model.ScopePersonal,
		PrimaryCurrencyAssetID: usd,
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Transaction{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Account{})
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	stockName := "Hypothetical EU Stock"
	stk := &model.Asset{Symbol: "EUSTK-" + time.Now().Format("150405.000000000"), Kind: model.AssetKindEquity, DisplayName: &stockName, Precision: 4, QuoteCurrencyAssetID: &eur}
	if err := g.Create(stk).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, stk.ID) })
	acct := &model.Account{
		UserID: u.ID, Name: "EU-" + time.Now().Format("150405.000000000"),
		InstitutionSlug: "fixture", AccountType: "investment", Currency: "EUR",
		PrimaryQuoteAssetID: eur,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}

	// No EUR→USD price seeded.
	createPair(t, g, u.ID, acct.ID, stk.ID, eur, "buy",
		decimal.NewFromInt(5), decimal.NewFromInt(50),
		time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))

	_, err := valuation.Recompute(context.Background(),
		repository.NewTransactionRepository(g),
		repository.NewPriceRepository(g),
		u.ID, acct.ID, stk.ID, usd,
	)
	if !errors.Is(err, valuation.ErrFXUnavailable) {
		t.Fatalf("err = %v, want ErrFXUnavailable", err)
	}
}

func TestRecompute_TransferInOut_QuantityOnly(t *testing.T) {
	// Inflow without a paired cash leg → quantity rises but cost basis
	// stays "unknown" for the added units. Outflow without a paired cash
	// leg → proportional cost-basis removal (we know lots leave).
	g := openTestDB(t)
	userID, accountID, _, aapl := seedTradeAccount(t, g)
	ctx := context.Background()

	// Transfer in 5 shares (unpaired).
	if err := g.Create(&model.Transaction{
		UserID: userID, AccountID: accountID, AssetID: aapl,
		Amount: decimal.NewFromInt(5), TransactionDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Source: "manual",
	}).Error; err != nil {
		t.Fatalf("seed transfer-in: %v", err)
	}

	res, err := valuation.Recompute(ctx,
		repository.NewTransactionRepository(g),
		repository.NewPriceRepository(g),
		userID, accountID, aapl, testutil.LookupUSDAssetID(t, g),
	)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if !res.Quantity.Equal(decimal.NewFromInt(5)) {
		t.Errorf("qty = %s, want 5", res.Quantity)
	}
	if res.HasCostBasis {
		t.Error("HasCostBasis = true, want false (no priced source)")
	}
}
