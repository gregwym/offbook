package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

func seedReconcileAccount(t *testing.T, g *gorm.DB, userID int64) (accountID, usdID int64) {
	t.Helper()
	usdID = testutil.LookupUSDAssetID(t, g)
	acct := &model.Account{
		UserID:              userID,
		Name:                "recon-" + time.Now().Format("150405.000000000"),
		InstitutionSlug:     "fixture",
		AccountType:         "checking",
		PrimaryQuoteAssetID: usdID,
		IsActive:            true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, acct.ID) })
	return acct.ID, usdID
}

// TestReconcilePosition_FirstIsOpeningBalance_ThenAdjustment proves the
// ADR-0017 invariant: ReconcilePosition makes Σ transactions == reported, the
// first reconciling row is opening_balance, later divergences are adjustments,
// and equal values are idempotent.
func TestReconcilePosition_FirstIsOpeningBalance_ThenAdjustment(t *testing.T) {
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	accountID, usdID := seedReconcileAccount(t, g, userID)
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	obsRepo := repository.NewBalanceObservationRepository(g)

	// First reconcile: no transactions yet; Plaid reports 1000 → opening_balance.
	rec, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(1000), time.Now().UTC(), "plaid")
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if rec == nil || rec.Kind != model.KindOpeningBalance || !rec.Amount.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("first reconcile = %+v, want opening_balance of 1000", rec)
	}
	if rec.Source != "system" {
		t.Errorf("reconciling source = %q, want system", rec.Source)
	}
	assertFold(t, txRepo, userID, accountID, usdID, "1000")

	// Idempotent: same value again → no row.
	rec, err = service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(1000), time.Now().UTC(), "plaid")
	if err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	if rec != nil {
		t.Errorf("expected no reconciling row when fold==reported, got %+v", rec)
	}

	// Divergence: Plaid now reports 1200 → adjustment of +200.
	rec, err = service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(1200), time.Now().UTC(), "plaid")
	if err != nil {
		t.Fatalf("divergence reconcile: %v", err)
	}
	if rec == nil || rec.Kind != model.KindAdjustment || !rec.Amount.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("divergence reconcile = %+v, want adjustment of 200", rec)
	}
	assertFold(t, txRepo, userID, accountID, usdID, "1200")

	// Every report is recorded as an observation (3 calls).
	obs, err := obsRepo.ListByAccountAsset(ctx, userID, accountID, usdID)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(obs) != 3 {
		t.Errorf("observations = %d, want 3", len(obs))
	}
}

func assertFold(t *testing.T, txRepo repository.TransactionRepository, userID, accountID, assetID int64, want string) {
	t.Helper()
	fold, err := txRepo.FoldQuantity(context.Background(), userID, accountID, assetID)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !fold.Equal(decimal.RequireFromString(want)) {
		t.Errorf("fold = %s, want %s", fold, want)
	}
}
