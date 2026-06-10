package prices_test

import (
	"context"
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
	"github.com/gregwym/offbook/backend/internal/service/prices"
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

func seedUser(t *testing.T, g *gorm.DB) int64 {
	t.Helper()
	u := &model.User{
		Email:                  fmt.Sprintf("prices-test-%d-%d@example.test", time.Now().UnixNano(), len(t.Name())),
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

func seedAccount(t *testing.T, g *gorm.DB, userID int64) *model.Account {
	t.Helper()
	a := &model.Account{
		UserID: userID, Name: "Prices-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "crypto", Currency: "USD",
		PrimaryQuoteAssetID: testutil.LookupUSDAssetID(t, g), IsActive: true,
	}
	if err := g.Create(a).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return a
}

func seedPosition(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, qty string) {
	t.Helper()
	p := &model.Position{
		UserID: userID, AccountID: accountID, AssetID: assetID,
		Quantity: decimal.RequireFromString(qty),
	}
	if err := g.Create(p).Error; err != nil {
		t.Fatalf("seed position: %v", err)
	}
}

// fakeProvider quotes every crypto asset at a fixed price and records what
// it was asked for, so tests can assert the egress set.
type fakeProvider struct {
	requested []model.Asset
	price     decimal.Decimal
	asOf      time.Time
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Supports(a model.Asset) bool { return a.Kind == model.AssetKindCrypto }

func (f *fakeProvider) Fetch(_ context.Context, assets []model.Asset, quote model.Asset) ([]prices.Quote, error) {
	f.requested = append(f.requested, assets...)
	out := make([]prices.Quote, 0, len(assets))
	for _, a := range assets {
		out = append(out, prices.Quote{AssetID: a.ID, QuoteAssetID: quote.ID, Price: f.price, AsOf: f.asOf})
	}
	return out, nil
}

// TestRefreshForUser_WritesHeldCryptoOnly: refresh prices exactly the
// crypto the user holds — fiat lands in Skipped (FX is Phase 2), the
// primary currency is excluded entirely, and the observation lands in
// `prices` with the provider as source.
func TestRefreshForUser_WritesHeldCryptoOnly(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userID := seedUser(t, g)
	acct := seedAccount(t, g, userID)

	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	btc := testutil.LookupAssetID(t, g, "BTC", "crypto")

	seedPosition(t, g, userID, acct.ID, usd, "1000") // primary currency → not refreshed, not skipped
	seedPosition(t, g, userID, acct.ID, eur, "100")  // fiat → skipped (Phase 2)
	seedPosition(t, g, userID, acct.ID, btc, "0.5")  // crypto → refreshed

	asOf := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	fake := &fakeProvider{price: decimal.RequireFromString("67000.5"), asOf: asOf}
	svc := prices.NewService(
		repository.NewUserRepository(g),
		repository.NewPositionRepository(g),
		repository.NewAssetRepository(g),
		repository.NewPriceRepository(g),
		fake,
	)
	t.Cleanup(func() {
		g.Unscoped().Where("source = ? AND as_of = ?", "fake", asOf).Delete(&model.Price{})
	})

	result, err := svc.RefreshForUser(ctx, userID)
	if err != nil {
		t.Fatalf("RefreshForUser: %v", err)
	}
	if result.Refreshed != 1 {
		t.Errorf("Refreshed = %d, want 1 (BTC only)", result.Refreshed)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "EUR" {
		t.Errorf("Skipped = %v, want [EUR]", result.Skipped)
	}
	if len(fake.requested) != 1 || fake.requested[0].ID != btc {
		t.Errorf("provider asked for %+v, want exactly the held BTC (egress = held symbols only)", fake.requested)
	}

	var row model.Price
	if err := g.Where("asset_id = ? AND quote_asset_id = ? AND source = ?", btc, usd, "fake").
		Order("as_of DESC").First(&row).Error; err != nil {
		t.Fatalf("price row not written: %v", err)
	}
	if row.Price.String() != "67000.5" {
		t.Errorf("stored price = %s, want 67000.5", row.Price)
	}

	// Idempotency: refreshing again with the same as_of upserts in place,
	// not a duplicate row.
	if _, err := svc.RefreshForUser(ctx, userID); err != nil {
		t.Fatalf("second RefreshForUser: %v", err)
	}
	var n int64
	if err := g.Model(&model.Price{}).
		Where("asset_id = ? AND quote_asset_id = ? AND source = ? AND as_of = ?", btc, usd, "fake", asOf).
		Count(&n).Error; err != nil {
		t.Fatalf("count price rows: %v", err)
	}
	if n != 1 {
		t.Errorf("price rows = %d, want 1 (same observation upserts, not duplicates)", n)
	}
}

// TestRefreshForUser_TenantScoped: another user's holdings never leave the
// box — the provider sees only the requesting user's assets.
func TestRefreshForUser_TenantScoped(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	userA := seedUser(t, g)
	userB := seedUser(t, g)
	acctB := seedAccount(t, g, userB)
	btc := testutil.LookupAssetID(t, g, "BTC", "crypto")
	seedPosition(t, g, userB, acctB.ID, btc, "1") // user B holds crypto; A holds nothing

	fake := &fakeProvider{price: decimal.New(1, 0), asOf: time.Now().UTC()}
	svc := prices.NewService(
		repository.NewUserRepository(g),
		repository.NewPositionRepository(g),
		repository.NewAssetRepository(g),
		repository.NewPriceRepository(g),
		fake,
	)

	result, err := svc.RefreshForUser(ctx, userA)
	if err != nil {
		t.Fatalf("RefreshForUser: %v", err)
	}
	if result.Refreshed != 0 || len(result.Skipped) != 0 {
		t.Errorf("result = %+v, want nothing refreshed or skipped (user A holds nothing)", result)
	}
	if len(fake.requested) != 0 {
		t.Errorf("provider was asked for %+v — user B's holdings leaked into user A's refresh", fake.requested)
	}
}
