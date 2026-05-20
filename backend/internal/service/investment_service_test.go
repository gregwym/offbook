package service_test

import (
	"context"
	"errors"
	"strings"
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

// portfolioCreate is a helper that constructs an investment with optional
// cost basis, market value, and asset class. It returns the created snapshot
// or fails the test.
func portfolioCreate(
	t *testing.T, svc *service.InvestmentService, userID, accountID int64,
	ticker string, qty int64, costBasis, marketValue *int64, assetClass *string, day int,
) {
	t.Helper()
	in := service.CreateInvestmentInput{
		AccountID:    accountID,
		Ticker:       ticker,
		Quantity:     decimal.NewFromInt(qty),
		AssetClass:   assetClass,
		SnapshotDate: time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC),
		Source:       "manual",
	}
	if costBasis != nil {
		cb := decimal.NewFromInt(*costBasis)
		in.CostBasis = &cb
	}
	if marketValue != nil {
		mv := decimal.NewFromInt(*marketValue)
		in.MarketValue = &mv
	}
	if _, err := svc.Create(context.Background(), userID, in); err != nil {
		t.Fatalf("seed %s: %v", ticker, err)
	}
}

func intPtr(v int64) *int64   { return &v }
func strPtr(s string) *string { return &s }

// TestInvestmentService_ListLatest_EmptyReturnsNonNilSlice guards against
// #180: an empty result was a nil slice, which encoding/json serialized as
// `null`. The frontend read `holdings.length` on that null and the page
// crashed before the empty state could render. The fix is in
// investment_repo.go (init with make([]…, 0)); this test pins the contract.
func TestInvestmentService_ListLatest_EmptyReturnsNonNilSlice(t *testing.T) {
	svc, userID, _, _ := newInvestmentSvc(t)

	rows, err := svc.ListLatest(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListLatest: %v", err)
	}
	if rows == nil {
		t.Fatal("ListLatest returned nil slice; must be non-nil so JSON renders [] not null")
	}
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0 for a user with no investments", len(rows))
	}
}

func TestInvestmentService_PortfolioSummary_EmptyReturnsZeros(t *testing.T) {
	svc, userID, _, _ := newInvestmentSvc(t)

	got, err := svc.PortfolioSummary(context.Background(), userID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !got.TotalMarketValue.IsZero() || !got.TotalCostBasis.IsZero() {
		t.Errorf("expected zero totals, got mv=%s cb=%s", got.TotalMarketValue, got.TotalCostBasis)
	}
	if got.TotalUnrealizedGainLoss != nil {
		t.Errorf("expected nil G/L on empty portfolio, got %s", *got.TotalUnrealizedGainLoss)
	}
	if got.HoldingsCount != 0 {
		t.Errorf("HoldingsCount = %d, want 0", got.HoldingsCount)
	}
	if len(got.ByAssetClass) != 0 {
		t.Errorf("ByAssetClass = %v, want empty", got.ByAssetClass)
	}
}

func TestInvestmentService_PortfolioSummary_TotalsAndAllocation(t *testing.T) {
	svc, userID, accountID, g := newInvestmentSvc(t)

	// VTI: cost=100, mv=150 (stock)
	portfolioCreate(t, svc, userID, accountID, "VTI", 10, intPtr(100), intPtr(150), strPtr("stock"), 1)
	// BND: cost=50, mv=40 (bond) — unrealized loss
	portfolioCreate(t, svc, userID, accountID, "BND", 5, intPtr(50), intPtr(40), strPtr("bond"), 1)
	// AAPL: cost null, mv=210 (stock) — partial data
	portfolioCreate(t, svc, userID, accountID, "AAPL", 3, nil, intPtr(210), strPtr("stock"), 1)
	// BTC: no asset class, no cost basis, no market value (just held)
	portfolioCreate(t, svc, userID, accountID, "BTC", 1, nil, nil, nil, 1)
	// Closed position — quantity zero. The service rejects zero on Create,
	// but Plaid/CSV imports could land one directly; insert via GORM to
	// exercise the "exclude closed positions" branch.
	cb := decimal.NewFromInt(999)
	mv := decimal.NewFromInt(999)
	ac := "stock"
	closed := &model.Investment{
		UserID:       userID,
		AccountID:    accountID,
		Ticker:       "GME",
		AssetClass:   &ac,
		Quantity:     decimal.Zero,
		CostBasis:    &cb,
		MarketValue:  &mv,
		SnapshotDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Source:       "manual",
	}
	if err := g.Create(closed).Error; err != nil {
		t.Fatalf("seed closed position: %v", err)
	}

	got, err := svc.PortfolioSummary(context.Background(), userID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}

	// HoldingsCount excludes the zero-quantity holding.
	if got.HoldingsCount != 4 {
		t.Errorf("HoldingsCount = %d, want 4", got.HoldingsCount)
	}
	// TotalMarketValue = 150 + 40 + 210 = 400 (BTC mv null adds 0)
	if !got.TotalMarketValue.Equal(decimal.NewFromInt(400)) {
		t.Errorf("TotalMarketValue = %s, want 400", got.TotalMarketValue)
	}
	// TotalCostBasis = 100 + 50 = 150 (AAPL/BTC excluded — nil)
	if !got.TotalCostBasis.Equal(decimal.NewFromInt(150)) {
		t.Errorf("TotalCostBasis = %s, want 150", got.TotalCostBasis)
	}
	// TotalUnrealizedGainLoss only sums holdings with both mv and cb:
	//   VTI: 150-100=50; BND: 40-50=-10  → 40
	if got.TotalUnrealizedGainLoss == nil {
		t.Fatalf("TotalUnrealizedGainLoss nil, want 40")
	}
	if !got.TotalUnrealizedGainLoss.Equal(decimal.NewFromInt(40)) {
		t.Errorf("TotalUnrealizedGainLoss = %s, want 40", *got.TotalUnrealizedGainLoss)
	}

	// Allocation: stock=360 (90%), bond=40 (10%), Unclassified=0 (0%).
	byClass := map[string]service.AssetClassAllocation{}
	for _, a := range got.ByAssetClass {
		byClass[a.AssetClass] = a
	}
	if got, want := byClass["stock"].MarketValue, decimal.NewFromInt(360); !got.Equal(want) {
		t.Errorf("stock mv = %s, want %s", got, want)
	}
	if got, want := byClass["stock"].WeightPct, decimal.NewFromInt(90); !got.Equal(want) {
		t.Errorf("stock weight = %s, want 90", got)
	}
	if got, want := byClass["bond"].MarketValue, decimal.NewFromInt(40); !got.Equal(want) {
		t.Errorf("bond mv = %s, want %s", got, want)
	}
	if got, want := byClass["bond"].WeightPct, decimal.NewFromInt(10); !got.Equal(want) {
		t.Errorf("bond weight = %s, want 10", got)
	}
	// BTC contributed 0 to mv so Unclassified bucket has 0 mv but is still
	// present (it has a non-null aggregate from the loop).
	if a, ok := byClass["Unclassified"]; !ok {
		t.Errorf("expected Unclassified bucket present, got %v", got.ByAssetClass)
	} else if !a.MarketValue.IsZero() || !a.WeightPct.IsZero() {
		t.Errorf("Unclassified = %+v, want zero mv + zero weight", a)
	}

	// Weights for non-zero classes sum to ~100%.
	sumWeight := decimal.Zero
	for _, a := range got.ByAssetClass {
		sumWeight = sumWeight.Add(a.WeightPct)
	}
	if !sumWeight.Equal(decimal.NewFromInt(100)) {
		t.Errorf("weight sum = %s, want 100", sumWeight)
	}
}

func TestInvestmentService_PortfolioSummary_GainLossNilWhenNoOverlap(t *testing.T) {
	// All holdings have either cost OR market value but never both — G/L
	// can't be computed, so it should be nil rather than 0.
	svc, userID, accountID, _ := newInvestmentSvc(t)
	portfolioCreate(t, svc, userID, accountID, "AAPL", 1, intPtr(100), nil, strPtr("stock"), 1)
	portfolioCreate(t, svc, userID, accountID, "VTI", 1, nil, intPtr(200), strPtr("stock"), 1)

	got, err := svc.PortfolioSummary(context.Background(), userID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.TotalUnrealizedGainLoss != nil {
		t.Errorf("TotalUnrealizedGainLoss = %s, want nil (no holding has both fields)", *got.TotalUnrealizedGainLoss)
	}
}

func TestInvestmentService_PortfolioSummary_MultiTenant(t *testing.T) {
	svc, userA, accountA, g := newInvestmentSvc(t)
	portfolioCreate(t, svc, userA, accountA, "VTI", 10, intPtr(100), intPtr(150), strPtr("stock"), 1)

	userB := seedTestUser(t, g)
	acctB := &model.Account{
		UserID: userB, Name: "InvFixture-B-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "investment", Currency: "USD",
	}
	if err := g.Create(acctB).Error; err != nil {
		t.Fatalf("seed acct B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userB).Delete(&model.Investment{})
		g.Unscoped().Delete(&model.Account{}, acctB.ID)
	})

	got, err := svc.PortfolioSummary(context.Background(), userB)
	if err != nil {
		t.Fatalf("summary user B: %v", err)
	}
	if got.HoldingsCount != 0 || !got.TotalMarketValue.IsZero() {
		t.Errorf("user B saw user A's data: %+v", got)
	}
}

func TestInvestmentService_ImportCSV_EndToEnd(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)
	csv := `Symbol,Description,Quantity,Last Price,Current Value,Cost Basis Total,Average Cost Basis,Type
AAPL,APPLE INC,10,$184,"$1,840.00","$1,500.00",$150,Cash
VTI,VANGUARD TOTAL,50,$240,"$12,000.00","$10,000.00",$200,Cash
`
	res, err := svc.ImportCSV(context.Background(), userID, accountID, strings.NewReader(csv))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 2 || res.Skipped != 0 || len(res.Errors) != 0 {
		t.Fatalf("got %+v, want imported=2 skipped=0", res)
	}
	holdings, err := svc.ListLatest(context.Background(), userID)
	if err != nil {
		t.Fatalf("list latest: %v", err)
	}
	if len(holdings) != 2 {
		t.Fatalf("len(holdings) = %d, want 2", len(holdings))
	}
	for _, h := range holdings {
		if h.Source != "csv" {
			t.Errorf("source = %q, want csv", h.Source)
		}
	}
}

func TestInvestmentService_ImportCSV_UnknownFormat(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)
	_, err := svc.ImportCSV(context.Background(), userID, accountID, strings.NewReader("Foo,Bar\n1,2\n"))
	if !errors.Is(err, service.ErrUnknownCSVFormat) {
		t.Errorf("err = %v, want ErrUnknownCSVFormat", err)
	}
}

func TestInvestmentService_ResolveInvestmentAccount(t *testing.T) {
	svc, userID, _, g := newInvestmentSvc(t)
	ctx := context.Background()

	// The fixture seeds one investment-typed account → should resolve.
	id, err := svc.ResolveInvestmentAccount(ctx, userID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id == 0 {
		t.Errorf("got id 0, want fixture id")
	}

	// Add a second investment-typed account → should fail with ErrMissingAccountID.
	second := &model.Account{
		UserID: userID, Name: "Second-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "investment", Currency: "USD",
	}
	if err := g.Create(second).Error; err != nil {
		t.Fatalf("seed second: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, second.ID) })

	if _, err := svc.ResolveInvestmentAccount(ctx, userID); !errors.Is(err, service.ErrMissingAccountID) {
		t.Errorf("err = %v, want ErrMissingAccountID", err)
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
