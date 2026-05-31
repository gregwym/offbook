package plaid_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// TestService_SyncTransactions_ReconcilesCashPosition proves the ADR-0017
// invariant for the Plaid cash path: after a drain, the transaction fold
// equals the cash position SyncAccounts pinned to Plaid's balance. The first
// reconcile writes an opening_balance anchor (balance − Σ flows); a later
// drift produces an adjustment. Every reported balance is recorded as an
// observation.
func TestService_SyncTransactions_ReconcilesCashPosition(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	usdID := testutil.LookupUSDAssetID(t, g)
	ctx := context.Background()

	const plaidAcctID = "pacct-recon-cash-1"
	const plaidItemID = "item-recon-cash-1"
	acct := &model.Account{
		UserID:              userID,
		Name:                "Recon Checking",
		InstitutionSlug:     "ins_test",
		AccountType:         "checking",
		PrimaryQuoteAssetID: usdID,
		PlaidAccountID:      &[]string{plaidAcctID}[0],
		PlaidItemID:         &[]string{plaidItemID}[0],
		IsActive:            true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.AccountBalanceObservation{})
	})

	// SyncAccounts would have pinned the cash position to Plaid's balance.
	posRepo := repository.NewPositionRepository(g)
	if err := posRepo.Upsert(ctx, &model.Position{
		UserID:    userID,
		AccountID: acct.ID,
		AssetID:   usdID,
		Quantity:  decimal.NewFromInt(5000),
	}); err != nil {
		t.Fatalf("seed cash position: %v", err)
	}

	srv, _ := fakeTxnsSyncServer(t, plaidAcctID)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), nil)

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{
		UserID:         userID,
		PlaidItemID:    plaidItemID,
		AccessTokenEnc: enc,
		Status:         "active",
	}
	if err := itemRepo.Create(ctx, item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo,
		repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g),
		posRepo, piiSvc, nil, g)

	// First drain: flows net to -5.43 + 2000 - 12.50 = 1982.07.
	// opening_balance = 5000 − 1982.07 = 3017.93; fold must equal the
	// position quantity (5000).
	if _, err := svc.SyncTransactions(ctx, userID, plaidItemID); err != nil {
		t.Fatalf("SyncTransactions: %v", err)
	}

	fold, err := txRepo.FoldQuantity(ctx, userID, acct.ID, usdID)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !fold.Equal(decimal.NewFromInt(5000)) {
		t.Errorf("fold = %s, want 5000 (== cash position)", fold)
	}

	var opening model.Transaction
	if err := g.Where("user_id = ? AND account_id = ? AND kind = ?",
		userID, acct.ID, model.KindOpeningBalance).First(&opening).Error; err != nil {
		t.Fatalf("expected an opening_balance row: %v", err)
	}
	if !opening.Amount.Equal(decimal.RequireFromString("3017.93")) {
		t.Errorf("opening_balance amount = %s, want 3017.93 (balance − Σflows)", opening.Amount)
	}
	if opening.Source != "system" {
		t.Errorf("opening_balance source = %q, want system", opening.Source)
	}

	// One observation recorded for the reported balance.
	obsRepo := repository.NewBalanceObservationRepository(g)
	obs, err := obsRepo.ListByAccountAsset(ctx, userID, acct.ID, usdID)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("observations after first sync = %d, want 1", len(obs))
	}

	// Drift: SyncAccounts re-pins the cash position to a new balance (5500).
	// The next drain (no new flows) must emit an adjustment of +500, not a
	// silent overwrite, keeping fold == position.
	if err := posRepo.Upsert(ctx, &model.Position{
		UserID:    userID,
		AccountID: acct.ID,
		AssetID:   usdID,
		Quantity:  decimal.NewFromInt(5500),
	}); err != nil {
		t.Fatalf("re-pin cash position: %v", err)
	}
	if _, err := svc.SyncTransactions(ctx, userID, plaidItemID); err != nil {
		t.Fatalf("SyncTransactions #2: %v", err)
	}

	fold, err = txRepo.FoldQuantity(ctx, userID, acct.ID, usdID)
	if err != nil {
		t.Fatalf("fold #2: %v", err)
	}
	if !fold.Equal(decimal.NewFromInt(5500)) {
		t.Errorf("fold after drift = %s, want 5500", fold)
	}

	var adjustments []model.Transaction
	if err := g.Where("user_id = ? AND account_id = ? AND kind = ?",
		userID, acct.ID, model.KindAdjustment).Find(&adjustments).Error; err != nil {
		t.Fatalf("load adjustments: %v", err)
	}
	if len(adjustments) != 1 || !adjustments[0].Amount.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("adjustments = %+v, want a single +500 row", adjustments)
	}

	// Idempotent: a third drain with the position unchanged adds no new
	// reconciling rows.
	if _, err := svc.SyncTransactions(ctx, userID, plaidItemID); err != nil {
		t.Fatalf("SyncTransactions #3: %v", err)
	}
	var reconCount int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND source = 'system'", userID).
		Count(&reconCount).Error; err != nil {
		t.Fatalf("count reconciling rows: %v", err)
	}
	if reconCount != 2 {
		t.Errorf("system reconciling rows = %d, want 2 (opening + 1 adjustment)", reconCount)
	}
}
