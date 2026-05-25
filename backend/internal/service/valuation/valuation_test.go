package valuation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

func loadRepoDotenv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	loadRepoDotenv()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no DATABASE_URL set; skipping integration test")
	}
	g, err := db.Open(url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx, g); err != nil {
		t.Skipf("db.Ping: %v; skipping integration test", err)
	}
	return g
}

func newSvc(t *testing.T) (*valuation.Service, *gorm.DB) {
	t.Helper()
	g := openTestDB(t)
	svc := valuation.NewService(
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewAssetRepository(g),
		repository.NewAccountRepository(g),
	)
	return svc, g
}

func seedUser(t *testing.T, g *gorm.DB, label string) int64 {
	t.Helper()
	u := &model.User{
		Email:                  fmt.Sprintf("val-%s-%d@example.test", label, time.Now().UnixNano()),
		PasswordHash:           "x",
		LastScope:              model.ScopePersonal,
		DefaultScope:           model.ScopePersonal,
		PrimaryCurrencyAssetID: testutil.LookupUSDAssetID(t, g),
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Position{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Account{})
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	return u.ID
}

func seedAccount(t *testing.T, g *gorm.DB, userID int64, label string, currency string) *model.Account {
	t.Helper()
	a := &model.Account{
		UserID:              userID,
		Name:                "val-" + label + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		InstitutionSlug:     "fixture",
		AccountType:         "checking",
		Currency:            currency,
		PrimaryQuoteAssetID: testutil.LookupAssetID(t, g, currency, "fiat"),
		IsActive:            true,
	}
	if err := g.Create(a).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, a.ID) })
	return a
}

func upsertPosition(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, quantity string) *model.Position {
	t.Helper()
	q, _ := decimal.NewFromString(quantity)
	p := &model.Position{UserID: userID, AccountID: accountID, AssetID: assetID, Quantity: q}
	if err := g.Create(p).Error; err != nil {
		t.Fatalf("seed position: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Position{}, p.ID) })
	return p
}

func insertPrice(t *testing.T, g *gorm.DB, assetID, quoteAssetID int64, price string, asOf time.Time) {
	t.Helper()
	pr, _ := decimal.NewFromString(price)
	p := &model.Price{AssetID: assetID, QuoteAssetID: quoteAssetID, Price: pr, AsOf: asOf, Source: "test"}
	if err := g.Create(p).Error; err != nil {
		t.Fatalf("seed price: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Price{}, p.ID) })
}

// TestValue_SameAsset returns quantity directly — the cheapest hop, with no
// price lookup and never stale.
func TestValue_SameAsset(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	userID := seedUser(t, g, "same")
	acct := seedAccount(t, g, userID, "chk", "USD")

	pos := upsertPosition(t, g, userID, acct.ID, usd, "1234.56")
	got, err := svc.Value(ctx, *pos, time.Now(), usd)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got.String() != "1234.56" {
		t.Errorf("Value = %q, want 1234.56", got.String())
	}
}

// TestValue_DirectPrice takes the asset → quote price as-is.
func TestValue_DirectPrice(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	userID := seedUser(t, g, "direct")
	acct := seedAccount(t, g, userID, "eur", "EUR")

	pos := upsertPosition(t, g, userID, acct.ID, eur, "100")
	insertPrice(t, g, eur, usd, "1.10", time.Now().Add(-time.Hour))

	got, err := svc.Value(ctx, *pos, time.Now(), usd)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got.String() != "110" {
		t.Errorf("Value = %q, want 110 (100 EUR × 1.10)", got.String())
	}
}

// TestValue_TwoHopFX walks asset → native quote → user quote when no direct
// price exists. AAPL is quoted natively in USD; we ask for the value in EUR.
func TestValue_TwoHopFX(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	// AAPL with native quote USD.
	aapl := &model.Asset{Symbol: "AAPL-VAL", Kind: model.AssetKindEquity, QuoteCurrencyAssetID: &usd, Precision: 8}
	if err := g.Create(aapl).Error; err != nil {
		t.Fatalf("seed AAPL: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, aapl.ID) })

	userID := seedUser(t, g, "twohop")
	acct := seedAccount(t, g, userID, "brk", "EUR")

	pos := upsertPosition(t, g, userID, acct.ID, aapl.ID, "10")
	asOf := time.Now()
	// AAPL → USD = 200; USD → EUR = 0.9. Value in EUR = 10 × 200 × 0.9 = 1800.
	insertPrice(t, g, aapl.ID, usd, "200", asOf.Add(-time.Hour))
	insertPrice(t, g, usd, eur, "0.9", asOf.Add(-time.Hour))

	got, err := svc.Value(ctx, *pos, asOf, eur)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got.String() != "1800" {
		t.Errorf("Value = %q, want 1800", got.String())
	}
}

// TestValue_StalePrice returns ErrStalePrice when the freshest observation
// is older than the configured window.
func TestValue_StalePrice(t *testing.T) {
	svc, g := newSvc(t)
	svc.WithStaleWindow(24 * time.Hour) // tight window so we can construct staleness
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	userID := seedUser(t, g, "stale")
	acct := seedAccount(t, g, userID, "eur", "EUR")

	pos := upsertPosition(t, g, userID, acct.ID, eur, "100")
	// Price from 30 days ago — well outside 24h window.
	insertPrice(t, g, eur, usd, "1.10", time.Now().Add(-30*24*time.Hour))

	_, err := svc.Value(ctx, *pos, time.Now(), usd)
	if !errors.Is(err, valuation.ErrStalePrice) {
		t.Fatalf("Value err = %v, want ErrStalePrice", err)
	}
}

// TestValue_NoPriceChain returns ErrStalePrice when neither direct nor
// two-hop pricing yields any observation. (Same sentinel — "no fresh
// chain" subsumes "no chain at all" for caller semantics.)
func TestValue_NoPriceChain(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	// Asset with no quote currency and no prices anywhere.
	tk := &model.Asset{Symbol: "ZZZ-VAL", Kind: model.AssetKindOther, Precision: 8}
	if err := g.Create(tk).Error; err != nil {
		t.Fatalf("seed ZZZ: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, tk.ID) })

	userID := seedUser(t, g, "nochain")
	acct := seedAccount(t, g, userID, "any", "USD")
	pos := upsertPosition(t, g, userID, acct.ID, tk.ID, "5")

	_, err := svc.Value(ctx, *pos, time.Now(), usd)
	if !errors.Is(err, valuation.ErrStalePrice) {
		t.Fatalf("Value err = %v, want ErrStalePrice", err)
	}
}

// TestAccountBalance_SumsPositions sums all positions in an account, with
// fresh prices returning a clean total and no stale-asset list.
func TestAccountBalance_SumsPositions(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	userID := seedUser(t, g, "balance-sum")
	acct := seedAccount(t, g, userID, "mixed", "USD")

	upsertPosition(t, g, userID, acct.ID, usd, "500.00")
	upsertPosition(t, g, userID, acct.ID, eur, "100")
	insertPrice(t, g, eur, usd, "1.20", time.Now().Add(-time.Hour))

	total, stale, err := svc.AccountBalance(ctx, userID, acct.ID, time.Now())
	if err != nil {
		t.Fatalf("AccountBalance: %v", err)
	}
	if total.String() != "620" {
		t.Errorf("total = %q, want 620 (500 USD + 100 EUR × 1.20)", total.String())
	}
	if len(stale) != 0 {
		t.Errorf("stale = %v, want empty", stale)
	}
}

// TestAccountBalance_StaleContributesZero — a stale-priced position
// contributes 0 to the total but appears in the stale list so the UI
// can warn.
func TestAccountBalance_StaleContributesZero(t *testing.T) {
	svc, g := newSvc(t)
	svc.WithStaleWindow(24 * time.Hour)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	userID := seedUser(t, g, "balance-stale")
	acct := seedAccount(t, g, userID, "mixed", "USD")

	upsertPosition(t, g, userID, acct.ID, usd, "500.00")
	upsertPosition(t, g, userID, acct.ID, eur, "100")
	// Stale EUR price.
	insertPrice(t, g, eur, usd, "1.20", time.Now().Add(-30*24*time.Hour))

	total, stale, err := svc.AccountBalance(ctx, userID, acct.ID, time.Now())
	if err != nil {
		t.Fatalf("AccountBalance: %v", err)
	}
	if total.String() != "500" {
		t.Errorf("total = %q, want 500 (stale EUR contributes 0)", total.String())
	}
	if len(stale) != 1 || stale[0] != eur {
		t.Errorf("stale = %v, want [%d]", stale, eur)
	}
}
