package service_test

import (
	"context"
	"errors"
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
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
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

func newAccountSvc(t *testing.T) (*service.AccountService, int64, *gorm.DB) {
	t.Helper()
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	return service.NewAccountService(g, repository.NewAccountRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g)), userID, g
}

// validInput returns a creator that produces fresh, valid create inputs so
// each subtest gets its own (Name varies by suffix so we don't collide across
// runs that share a DB).
func validInput(suffix string) service.CreateAccountInput {
	return service.CreateAccountInput{
		Name:            "AcctSvc " + suffix,
		InstitutionSlug: "fixture",
		AccountType:     "checking",
		Currency:        "USD",
		OpeningBalance:  decimal.NewFromInt(0),
	}
}

func TestAccountService_Create_Validation(t *testing.T) {
	svc, userID, _ := newAccountSvc(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		mutate  func(*service.CreateAccountInput)
		wantErr error
	}{
		{
			name:    "valid input succeeds",
			mutate:  func(*service.CreateAccountInput) {},
			wantErr: nil,
		},
		{
			name:    "empty name rejected",
			mutate:  func(in *service.CreateAccountInput) { in.Name = "   " },
			wantErr: service.ErrEmptyName,
		},
		{
			name:    "empty institution rejected",
			mutate:  func(in *service.CreateAccountInput) { in.InstitutionSlug = "" },
			wantErr: service.ErrEmptyInstitution,
		},
		{
			name:    "bogus account_type rejected",
			mutate:  func(in *service.CreateAccountInput) { in.AccountType = "bogus" },
			wantErr: service.ErrInvalidType,
		},
		{
			name:    "currency too long rejected",
			mutate:  func(in *service.CreateAccountInput) { in.Currency = "USDD" },
			wantErr: service.ErrInvalidCurrency,
		},
		{
			name: "last_four non-numeric rejected",
			mutate: func(in *service.CreateAccountInput) {
				v := "12ab"
				in.LastFour = &v
			},
			wantErr: service.ErrInvalidLastFour,
		},
		{
			name: "last_four wrong length rejected",
			mutate: func(in *service.CreateAccountInput) {
				v := "12"
				in.LastFour = &v
			},
			wantErr: service.ErrInvalidLastFour,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(t.Name() + "-" + tcSuffix(i))
			tc.mutate(&in)
			got, err := svc.Create(ctx, userID, in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("Create err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got == nil || got.ID == 0 {
				t.Fatalf("expected created account, got %+v", got)
			}
			t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, got.ID) })
		})
	}
}

func TestAccountService_SoftDelete_Idempotent(t *testing.T) {
	svc, userID, _ := newAccountSvc(t)
	ctx := context.Background()

	acc, err := svc.Create(ctx, userID, validInput("delete-idem"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.SoftDelete(ctx, userID, acc.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	err = svc.SoftDelete(ctx, userID, acc.ID)
	if !errors.Is(err, service.ErrAccountNotFound) {
		t.Errorf("second delete err = %v, want ErrAccountNotFound", err)
	}

	if _, err := svc.Get(ctx, userID, acc.ID); !errors.Is(err, service.ErrAccountNotFound) {
		t.Errorf("Get after delete err = %v, want ErrAccountNotFound", err)
	}
}

// TestAccountService_Get_TenantIsolation enforces the multi-tenant rule:
// fetching another user's account returns ErrAccountNotFound.
func TestAccountService_Get_TenantIsolation(t *testing.T) {
	svc, userID, g := newAccountSvc(t)
	ctx := context.Background()

	acc, err := svc.Create(ctx, userID, validInput("tenant-iso"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, acc.ID) })

	other := seedTestUser(t, g)
	if _, err := svc.Get(ctx, other, acc.ID); !errors.Is(err, service.ErrAccountNotFound) {
		t.Errorf("cross-tenant Get err = %v, want ErrAccountNotFound", err)
	}
}

// TestAccountService_Update_DoesNotTouchPII is the PII isolation contract
// test for the account service: an Update on an account must leave its
// pii_store rows untouched, since the service deliberately does not depend
// on pii_repo. This is the architectural cornerstone of the privacy model.
func TestAccountService_Update_DoesNotTouchPII(t *testing.T) {
	svc, userID, g := newAccountSvc(t)
	ctx := context.Background()

	acc, err := svc.Create(ctx, userID, validInput("pii-isolation"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, acc.ID) })

	// Manually insert a PII row outside the service so we know exactly what's
	// in the table before/after the update. (Going through PIIService would
	// also work but obscures what we're testing.)
	pii := model.PIIRecord{
		EntityType: "account",
		EntityID:   acc.ID,
		FieldName:  "holder_name",
		Value:      "Jane Doe Test",
	}
	if err := g.WithContext(ctx).Create(&pii).Error; err != nil {
		t.Fatalf("seed pii: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("entity_type = ? AND entity_id = ?", "account", acc.ID).
			Delete(&model.PIIRecord{})
	})

	// Update everything we can about the account that is NOT PII.
	newName := "Renamed via update"
	newType := "savings"
	newCurr := "EUR"
	inactive := false
	if _, err := svc.Update(ctx, userID, acc.ID, service.UpdateAccountInput{
		Name:        &newName,
		AccountType: &newType,
		Currency:    &newCurr,
		IsActive:    &inactive,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// PII row must be byte-identical to what we seeded.
	var after model.PIIRecord
	if err := g.WithContext(ctx).First(&after, pii.ID).Error; err != nil {
		t.Fatalf("re-fetch pii: %v", err)
	}
	if after.Value != "Jane Doe Test" || after.FieldName != "holder_name" {
		t.Errorf("PII mutated by account update: got %+v", after)
	}
}

func tcSuffix(i int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return string(letters[i%len(letters)]) + time.Now().Format("150405.000000")
}

// TestAccountService_CurrencyDerivedFromAsset proves currency is no longer a
// stored column (#284): after Create, Get/List resolve it from the account's
// primary quote asset symbol, not a persisted accounts.currency value.
func TestAccountService_CurrencyDerivedFromAsset(t *testing.T) {
	svc, userID, _ := newAccountSvc(t)
	ctx := context.Background()

	in := validInput(tcSuffix(0))
	in.Currency = "EUR"
	created, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Currency != "EUR" {
		t.Errorf("create response currency = %q, want EUR", created.Currency)
	}

	// Read back via the service — currency must be hydrated from the asset,
	// since the accounts.currency column no longer exists.
	got, err := svc.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Currency != "EUR" {
		t.Errorf("get currency = %q, want EUR (derived from primary_quote_asset)", got.Currency)
	}
	if got.PrimaryQuoteAssetID == 0 {
		t.Error("primary_quote_asset_id not set")
	}
}

// TestAccountService_Create_OpeningBalanceIsLedgerFact: an opening balance is
// recorded as an opening_balance transaction (ADR-0017) that counts toward
// net worth but is excluded from income/spending flow analytics. (#293)
func TestAccountService_Create_OpeningBalanceIsLedgerFact(t *testing.T) {
	svc, userID, g := newAccountSvc(t)
	ctx := context.Background()

	in := validInput(tcSuffix(7))
	in.OpeningBalance = decimal.NewFromInt(1000)
	acct, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The anchor is a real ledger row tagged opening_balance.
	var tx model.Transaction
	if err := g.Where("account_id = ? AND kind = ?", acct.ID, model.KindOpeningBalance).
		First(&tx).Error; err != nil {
		t.Fatalf("opening_balance transaction not found: %v", err)
	}
	if !tx.Amount.Equal(decimal.NewFromInt(1000)) {
		t.Errorf("opening_balance amount = %s, want 1000", tx.Amount)
	}

	// Counts toward net worth (positions × price) but NOT income/spending.
	// Net worth comes from the service since #344 (single valuation
	// derivation; the repo no longer computes it).
	dashSvc := service.NewDashboardService(
		repository.NewDashboardRepository(g),
		repository.NewTransactionRepository(g),
		repository.NewUserRepository(g),
		valuation.NewService(
			repository.NewPositionRepository(g),
			repository.NewPriceRepository(g),
			repository.NewAssetRepository(g),
			repository.NewAccountRepository(g),
		),
	)
	summary, err := dashSvc.Summarize(ctx, userID, service.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.NetWorth != "1000" {
		t.Errorf("net worth = %s, want 1000 (opening balance counts)", summary.NetWorth)
	}
	if !summary.NetWorthComplete {
		t.Error("net_worth_complete = false, want true (primary-currency cash needs no price)")
	}
	if summary.Income != "0" || summary.Spending != "0" {
		t.Errorf("income=%s spending=%s, want 0/0 (opening_balance excluded from flow)", summary.Income, summary.Spending)
	}
}

// TestListResponse_DerivedBalance verifies the accounts API serves the
// valuation-derived balance (#291): the opening cash position values at
// quantity in the account's own quote asset, and an unpriced position flips
// balance_complete to false instead of silently contributing $0 (#282).
func TestListResponse_DerivedBalance(t *testing.T) {
	svc, userID, g := newAccountSvc(t)
	svc.WithValuation(valuation.NewService(
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewAssetRepository(g),
		repository.NewAccountRepository(g),
	))
	ctx := context.Background()

	in := validInput(tcSuffix(8))
	in.OpeningBalance = decimal.RequireFromString("123.45")
	acct, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	find := func(t *testing.T) service.AccountResponse {
		t.Helper()
		resps, _, err := svc.ListResponse(ctx, userID, repository.AccountFilter{})
		if err != nil {
			t.Fatalf("list response: %v", err)
		}
		for _, r := range resps {
			if r.ID == acct.ID {
				return r
			}
		}
		t.Fatalf("account %d not in list response", acct.ID)
		return service.AccountResponse{}
	}

	t.Run("cash position values at quantity", func(t *testing.T) {
		r := find(t)
		if !r.Balance.Equal(decimal.RequireFromString("123.45")) {
			t.Errorf("balance = %s, want 123.45", r.Balance)
		}
		if !r.BalanceComplete {
			t.Error("balance_complete = false, want true (same-asset cash needs no price)")
		}
	})

	t.Run("unpriced position flips balance_complete", func(t *testing.T) {
		assetRepo := repository.NewAssetRepository(g)
		btc, err := assetRepo.EnsureBySymbolKind(ctx, "BTC", model.AssetKindCrypto, "Bitcoin")
		if err != nil {
			t.Fatalf("ensure BTC asset: %v", err)
		}
		posRepo := repository.NewPositionRepository(g)
		if err := posRepo.Upsert(ctx, &model.Position{
			UserID:    userID,
			AccountID: acct.ID,
			AssetID:   btc.ID,
			Quantity:  decimal.RequireFromString("0.5"),
		}); err != nil {
			t.Fatalf("upsert BTC position: %v", err)
		}

		r := find(t)
		if !r.Balance.Equal(decimal.RequireFromString("123.45")) {
			t.Errorf("balance = %s, want 123.45 (unpriced BTC excluded, not $0-coerced)", r.Balance)
		}
		if r.BalanceComplete {
			t.Error("balance_complete = true, want false (BTC has no price chain)")
		}

		single, err := svc.GetResponse(ctx, userID, acct.ID)
		if err != nil {
			t.Fatalf("get response: %v", err)
		}
		if !single.Balance.Equal(r.Balance) || single.BalanceComplete != r.BalanceComplete {
			t.Errorf("GetResponse balance=%s complete=%v, want %s/%v (must match ListResponse)",
				single.Balance, single.BalanceComplete, r.Balance, r.BalanceComplete)
		}
	})
}
