package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
)

// TestPortfolioSummary_RecentChange_NoPriorIsNil — fresh user with only
// one snapshot per holding has no pair to compare; RecentChange stays nil.
func TestPortfolioSummary_RecentChange_NoPriorIsNil(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)
	portfolioCreate(t, svc, userID, accountID, "VTI", 10, intPtr(100), intPtr(150), strPtr("stock"), 1)

	got, err := svc.PortfolioSummary(context.Background(), userID)
	if err != nil {
		t.Fatalf("PortfolioSummary: %v", err)
	}
	if got.RecentChange != nil {
		t.Fatalf("RecentChange = %+v, want nil (only one snapshot)", got.RecentChange)
	}
}

// TestPortfolioSummary_RecentChange_PairedHoldings sums deltas across
// holdings with two snapshots; counts up / down / flat.
func TestPortfolioSummary_RecentChange_PairedHoldings(t *testing.T) {
	svc, userID, accountID, _ := newInvestmentSvc(t)

	// VTI: 150 → 180 (up by 30)
	portfolioCreate(t, svc, userID, accountID, "VTI", 10, intPtr(100), intPtr(150), strPtr("stock"), 1)
	portfolioCreate(t, svc, userID, accountID, "VTI", 10, intPtr(100), intPtr(180), strPtr("stock"), 5)
	// BND: 50 → 45 (down by 5)
	portfolioCreate(t, svc, userID, accountID, "BND", 5, intPtr(50), intPtr(50), strPtr("bond"), 1)
	portfolioCreate(t, svc, userID, accountID, "BND", 5, intPtr(50), intPtr(45), strPtr("bond"), 5)
	// AAPL: 200 → 200 (flat)
	portfolioCreate(t, svc, userID, accountID, "AAPL", 3, intPtr(180), intPtr(200), strPtr("stock"), 1)
	portfolioCreate(t, svc, userID, accountID, "AAPL", 3, intPtr(180), intPtr(200), strPtr("stock"), 5)
	// GME: single snapshot — must not contribute.
	portfolioCreate(t, svc, userID, accountID, "GME", 1, intPtr(20), intPtr(15), strPtr("stock"), 5)

	got, err := svc.PortfolioSummary(context.Background(), userID)
	if err != nil {
		t.Fatalf("PortfolioSummary: %v", err)
	}
	if got.RecentChange == nil {
		t.Fatalf("RecentChange nil, want populated")
	}
	rc := got.RecentChange
	// Delta = (180-150) + (45-50) + (200-200) = 30 - 5 + 0 = 25
	if !rc.Delta.Equal(decimal.NewFromInt(25)) {
		t.Errorf("Delta = %s, want 25", rc.Delta)
	}
	if rc.HoldingsCompared != 3 {
		t.Errorf("HoldingsCompared = %d, want 3 (GME single-snapshot excluded)", rc.HoldingsCompared)
	}
	if rc.Up != 1 || rc.Down != 1 || rc.Flat != 1 {
		t.Errorf("up/down/flat = %d/%d/%d, want 1/1/1", rc.Up, rc.Down, rc.Flat)
	}
	if rc.LatestDate.Day() != 5 || rc.PriorDate.Day() != 1 {
		t.Errorf("dates = %s / %s, want day-5 latest and day-1 prior",
			rc.LatestDate.Format("2006-01-02"), rc.PriorDate.Format("2006-01-02"))
	}
}

// TestPortfolioSummary_RecentChange_TenantIsolation — user B's snapshots
// must not feed user A's recent change.
func TestPortfolioSummary_RecentChange_TenantIsolation(t *testing.T) {
	svc, userA, accountA, g := newInvestmentSvc(t)
	portfolioCreate(t, svc, userA, accountA, "VTI", 10, intPtr(100), intPtr(150), strPtr("stock"), 1)
	portfolioCreate(t, svc, userA, accountA, "VTI", 10, intPtr(100), intPtr(155), strPtr("stock"), 5)

	// Spin up a sibling user with a much larger swing on the same ticker.
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
	portfolioCreate(t, svc, userB, acctB.ID, "VTI", 10, intPtr(100), intPtr(150), strPtr("stock"), 1)
	portfolioCreate(t, svc, userB, acctB.ID, "VTI", 10, intPtr(100), intPtr(900), strPtr("stock"), 5)

	got, err := svc.PortfolioSummary(context.Background(), userA)
	if err != nil {
		t.Fatalf("PortfolioSummary: %v", err)
	}
	if got.RecentChange == nil {
		t.Fatalf("RecentChange nil")
	}
	if !got.RecentChange.Delta.Equal(decimal.NewFromInt(5)) {
		t.Errorf("user A Delta = %s, want 5 (B's 750 must not leak)", got.RecentChange.Delta)
	}
}
