package model_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
)

// loadRepoDotenv walks upward from cwd looking for a .env at the repo root
// (sibling of go.mod) and loads it. `go test` sets cwd to the package dir,
// so config.Load()'s default lookup of ./.env and ../.env won't find it.
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

// Verifies shopspring/decimal round-trips losslessly through GORM and
// PostgreSQL NUMERIC(30,18) — the foundation of every monetary calculation
// in the app. Skipped when no DATABASE_URL is reachable.

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
		t.Skipf("postgres unreachable at %s: %v", url, err)
	}
	t.Cleanup(func() { _ = db.Close(g) })
	return g
}

func TestDecimalRoundTrip_18Places(t *testing.T) {
	g := openTestDB(t)

	cases := []struct {
		name string
		in   string
	}{
		{"eighteen_places", "123456789012.345678901234567890"},
		{"one_wei", "0.000000000000000001"},
		{"negative_18", "-987654321098.765432109876543210"},
		{"zero", "0"},
		{"large_btc_amount", "0.051234567890123456"},
	}

	acct := newTestAccount(t, g)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := decimal.NewFromString(tc.in)
			if err != nil {
				t.Fatalf("parse input: %v", err)
			}
			desc := "decimal round-trip test"
			tx := model.Transaction{
				AccountID:       acct.ID,
				Amount:          want,
				Currency:        "USD",
				TransactionDate: time.Now().UTC().Truncate(24 * time.Hour),
				Source:          "manual",
				Description:     &desc,
			}
			if err := g.Create(&tx).Error; err != nil {
				t.Fatalf("insert tx: %v", err)
			}
			t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })

			var got model.Transaction
			if err := g.First(&got, tx.ID).Error; err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !got.Amount.Equal(want) {
				t.Fatalf("round-trip mismatch: want %s, got %s", want.String(), got.Amount.String())
			}
		})
	}
}

func TestDecimalArithmetic_PreservesPrecision(t *testing.T) {
	g := openTestDB(t)
	acct := newTestAccount(t, g)

	// Three high-precision amounts whose sum crosses carries through all 18 digits.
	a := decimal.RequireFromString("0.111111111111111111")
	b := decimal.RequireFromString("0.222222222222222222")
	c := decimal.RequireFromString("0.666666666666666667")
	want := a.Add(b).Add(c) // 1.000000000000000000

	ids := make([]int64, 0, 3)
	for _, amt := range []decimal.Decimal{a, b, c} {
		tx := model.Transaction{
			AccountID:       acct.ID,
			Amount:          amt,
			Currency:        "USD",
			TransactionDate: time.Now().UTC().Truncate(24 * time.Hour),
			Source:          "manual",
		}
		if err := g.Create(&tx).Error; err != nil {
			t.Fatalf("insert: %v", err)
		}
		ids = append(ids, tx.ID)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			g.Unscoped().Delete(&model.Transaction{}, id)
		}
	})

	var rows []model.Transaction
	if err := g.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	sum := decimal.Zero
	for _, r := range rows {
		sum = sum.Add(r.Amount)
	}
	if !sum.Equal(want) {
		t.Fatalf("sum mismatch: want %s, got %s", want.String(), sum.String())
	}

	// Multiply round-trip: 1 wei × 1_000_000 = 0.000000000001
	wei := decimal.RequireFromString("0.000000000000000001")
	product := wei.Mul(decimal.NewFromInt(1_000_000))
	tx := model.Transaction{
		AccountID:       acct.ID,
		Amount:          product,
		Currency:        "USD",
		TransactionDate: time.Now().UTC().Truncate(24 * time.Hour),
		Source:          "manual",
	}
	if err := g.Create(&tx).Error; err != nil {
		t.Fatalf("insert product: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })

	var back model.Transaction
	if err := g.First(&back, tx.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !back.Amount.Equal(product) {
		t.Fatalf("multiply round-trip mismatch: want %s, got %s", product.String(), back.Amount.String())
	}
}

func newTestAccount(t *testing.T, g *gorm.DB) model.Account {
	t.Helper()
	acct := model.Account{
		Name:            "decimal-test-" + time.Now().UTC().Format("150405.000000000"),
		InstitutionSlug: "test",
		AccountType:     "checking",
		Currency:        "USD",
		Balance:         decimal.Zero,
		IsActive:        true,
	}
	if err := g.Create(&acct).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, acct.ID) })
	return acct
}
