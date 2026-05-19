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
)

func newInvestmentSvc(t *testing.T) (svc *service.InvestmentService, userID, accountID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "InvFixture-" + suffix, InstitutionSlug: "fixture",
		AccountType: "investment", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Investment{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})

	svc = service.NewInvestmentService(
		repository.NewInvestmentRepository(g),
		repository.NewAccountRepository(g),
	)
	return svc, userID, acc.ID, g
}

func TestInvestmentService_Create_Validation(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)
	ctx := context.Background()

	costBasis := decimal.NewFromInt(100)
	negative := decimal.NewFromInt(-5)
	marketValue := decimal.NewFromInt(150)

	cases := []struct {
		name    string
		in      service.CreateInvestmentInput
		wantErr error
	}{
		{
			"valid manual",
			service.CreateInvestmentInput{
				AccountID:    accountID,
				Ticker:       "vti",
				Quantity:     decimal.NewFromInt(10),
				CostBasis:    &costBasis,
				MarketValue:  &marketValue,
				SnapshotDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				Source:       "manual",
			},
			nil,
		},
		{
			"empty ticker",
			service.CreateInvestmentInput{
				AccountID: accountID, Ticker: "  ", Quantity: decimal.NewFromInt(1), Source: "manual",
			},
			service.ErrInvalidTicker,
		},
		{
			"zero quantity",
			service.CreateInvestmentInput{
				AccountID: accountID, Ticker: "AAPL", Quantity: decimal.Zero, Source: "manual",
			},
			service.ErrZeroQuantity,
		},
		{
			"bad source",
			service.CreateInvestmentInput{
				AccountID: accountID, Ticker: "AAPL", Quantity: decimal.NewFromInt(1), Source: "ofx",
			},
			service.ErrInvalidInvestmentSrc,
		},
		{
			"negative cost basis",
			service.CreateInvestmentInput{
				AccountID: accountID, Ticker: "AAPL", Quantity: decimal.NewFromInt(1),
				CostBasis: &negative, Source: "manual",
			},
			service.ErrNegativeCostBasis,
		},
		{
			"negative market value",
			service.CreateInvestmentInput{
				AccountID: accountID, Ticker: "AAPL", Quantity: decimal.NewFromInt(1),
				MarketValue: &negative, Source: "manual",
			},
			service.ErrNegativeMarketValue,
		},
		{
			"unknown account",
			service.CreateInvestmentInput{
				AccountID: 9_999_999, Ticker: "AAPL", Quantity: decimal.NewFromInt(1), Source: "manual",
			},
			service.ErrAccountNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv, err := svc.Create(ctx, userID, tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if inv == nil || inv.ID == 0 {
				t.Fatalf("got %+v, want created snapshot", inv)
			}
			if inv.Ticker != "VTI" {
				t.Errorf("ticker = %q, want %q (should be uppercased)", inv.Ticker, "VTI")
			}
		})
	}
}

func TestInvestmentService_Create_CryptoPrecisionPreserved(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)
	ctx := context.Background()

	// 18 decimal places — example from the issue. Floats would lose digits.
	qty, err := decimal.NewFromString("0.051234567890123450")
	if err != nil {
		t.Fatalf("decimal parse: %v", err)
	}

	inv, err := svc.Create(ctx, userID, service.CreateInvestmentInput{
		AccountID:    accountID,
		Ticker:       "BTC",
		Quantity:     qty,
		SnapshotDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Source:       "manual",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get(ctx, userID, inv.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Quantity.Equal(qty) {
		t.Errorf("quantity round-trip: got %s, want %s", got.Quantity.String(), qty.String())
	}
}

func TestInvestmentService_ListLatest_PicksFreshestPerHolding(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)
	ctx := context.Background()

	mk := func(ticker string, qty int64, day int) {
		t.Helper()
		_, err := svc.Create(ctx, userID, service.CreateInvestmentInput{
			AccountID:    accountID,
			Ticker:       ticker,
			Quantity:     decimal.NewFromInt(qty),
			SnapshotDate: time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC),
			Source:       "manual",
		})
		if err != nil {
			t.Fatalf("seed %s @ day %d: %v", ticker, day, err)
		}
	}
	// VTI: 3 snapshots; newest (day 10) qty=15
	mk("VTI", 10, 1)
	mk("VTI", 12, 5)
	mk("VTI", 15, 10)
	// AAPL: 2 snapshots; newest (day 8) qty=20
	mk("AAPL", 5, 3)
	mk("AAPL", 20, 8)

	rows, err := svc.ListLatest(ctx, userID)
	if err != nil {
		t.Fatalf("list latest: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2 (one per ticker)", len(rows))
	}
	got := map[string]decimal.Decimal{}
	for _, r := range rows {
		got[r.Ticker] = r.Quantity
	}
	if v, ok := got["VTI"]; !ok || !v.Equal(decimal.NewFromInt(15)) {
		t.Errorf("VTI latest = %v, want 15", v)
	}
	if v, ok := got["AAPL"]; !ok || !v.Equal(decimal.NewFromInt(20)) {
		t.Errorf("AAPL latest = %v, want 20", v)
	}
}

func TestInvestmentService_ListSnapshots_ReturnsHistoryAscending(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)
	ctx := context.Background()

	for i, day := range []int{5, 1, 10, 3} {
		_, err := svc.Create(ctx, userID, service.CreateInvestmentInput{
			AccountID:    accountID,
			Ticker:       "VTI",
			Quantity:     decimal.NewFromInt(int64(10 + i)),
			SnapshotDate: time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC),
			Source:       "manual",
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Case-insensitive ticker match.
	rows, err := svc.ListSnapshots(ctx, userID, accountID, "vti")
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("len = %d, want 4", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].SnapshotDate.Before(rows[i-1].SnapshotDate) {
			t.Errorf("rows not ascending: idx %d (%s) before idx %d (%s)",
				i, rows[i].SnapshotDate, i-1, rows[i-1].SnapshotDate)
		}
	}
}

// TestInvestmentService_MultiTenant_ReadIsolation verifies that one user
// cannot see another user's snapshots — neither via GetByID, ListLatest, nor
// ListSnapshots.
func TestInvestmentService_MultiTenant_ReadIsolation(t *testing.T) {
	svc, userA, accountA, g := newInvestmentSvc(t)
	ctx := context.Background()

	// Seed user A's snapshot.
	invA, err := svc.Create(ctx, userA, service.CreateInvestmentInput{
		AccountID:    accountA,
		Ticker:       "VTI",
		Quantity:     decimal.NewFromInt(10),
		SnapshotDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Source:       "manual",
	})
	if err != nil {
		t.Fatalf("seed user A: %v", err)
	}

	// Build a parallel user B with their own account.
	userB := seedTestUser(t, g)
	acctB := &model.Account{
		UserID: userB, Name: "InvFixture-B-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "investment", Currency: "USD",
	}
	if err := g.Create(acctB).Error; err != nil {
		t.Fatalf("seed account B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userB).Delete(&model.Investment{})
		g.Unscoped().Delete(&model.Account{}, acctB.ID)
	})

	// GetByID — user B reading user A's snapshot must 404.
	if _, err := svc.Get(ctx, userB, invA.ID); !errors.Is(err, service.ErrInvestmentNotFound) {
		t.Errorf("Get cross-user: err = %v, want ErrInvestmentNotFound", err)
	}

	// ListLatest — user B sees none.
	if rows, err := svc.ListLatest(ctx, userB); err != nil || len(rows) != 0 {
		t.Errorf("ListLatest user B: rows=%d err=%v, want empty", len(rows), err)
	}

	// ListSnapshots — user B passing user A's account_id must fail account
	// ownership check before touching investment data.
	if _, err := svc.ListSnapshots(ctx, userB, accountA, "VTI"); !errors.Is(err, service.ErrAccountNotFound) {
		t.Errorf("ListSnapshots cross-user account: err = %v, want ErrAccountNotFound", err)
	}
}
