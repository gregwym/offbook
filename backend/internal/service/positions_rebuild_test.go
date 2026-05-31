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
)

func seedLedgerTx(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, kind, amount string) {
	t.Helper()
	desc := kind
	row := &model.Transaction{
		UserID:          userID,
		AccountID:       accountID,
		AssetID:         assetID,
		Kind:            kind,
		Amount:          decimal.RequireFromString(amount),
		Description:     &desc,
		TransactionDate: time.Now().UTC(),
		Source:          "manual",
	}
	if err := g.Create(row).Error; err != nil {
		t.Fatalf("seed %s txn: %v", kind, err)
	}
}

// TestRebuildPositions_MaterializesFoldPerPair proves the ADR-0017 invariant
// that positions is a regenerable fold of the ledger: after RebuildPositions,
// positions.quantity == Σ transactions.amount for every (account, asset),
// even when the stored position was wrong beforehand.
func TestRebuildPositions_MaterializesFoldPerPair(t *testing.T) {
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	accountID, usdID := seedReconcileAccount(t, g, userID)
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	posRepo := repository.NewPositionRepository(g)

	// Cash ledger folding to 5000: an opening_balance anchor plus three flows.
	seedLedgerTx(t, g, userID, accountID, usdID, model.KindOpeningBalance, "3017.93")
	seedLedgerTx(t, g, userID, accountID, usdID, model.KindFlow, "-5.43")
	seedLedgerTx(t, g, userID, accountID, usdID, model.KindFlow, "2000")
	seedLedgerTx(t, g, userID, accountID, usdID, model.KindFlow, "-12.50")

	// Corrupt the cached position so the rebuild has something to correct.
	if err := posRepo.Upsert(ctx, &model.Position{
		UserID: userID, AccountID: accountID, AssetID: usdID, Quantity: decimal.NewFromInt(999),
	}); err != nil {
		t.Fatalf("seed corrupted position: %v", err)
	}

	prices := repository.NewPriceRepository(g)
	users := repository.NewUserRepository(g)

	res, err := service.RebuildPositions(ctx, g, prices, users, userID)
	if err != nil {
		t.Fatalf("RebuildPositions: %v", err)
	}
	if res.Pairs != 1 || res.Updated != 1 {
		t.Errorf("result = %+v, want Pairs=1 Updated=1", res)
	}

	// The canonical invariant: position == fold.
	fold, err := txRepo.FoldQuantity(ctx, userID, accountID, usdID)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !fold.Equal(decimal.NewFromInt(5000)) {
		t.Fatalf("fold = %s, want 5000 (test setup)", fold)
	}
	positions, err := posRepo.ListByAccountID(ctx, userID, accountID)
	if err != nil {
		t.Fatalf("load positions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].Quantity.Equal(fold) {
		t.Errorf("position.quantity = %s, want %s (== fold)", positions[0].Quantity, fold)
	}

	// Idempotent: a second pass changes nothing.
	if _, err := service.RebuildPositions(ctx, g, prices, users, userID); err != nil {
		t.Fatalf("RebuildPositions #2: %v", err)
	}
	positions, _ = posRepo.ListByAccountID(ctx, userID, accountID)
	if len(positions) != 1 || !positions[0].Quantity.Equal(fold) {
		t.Errorf("after re-run, position = %+v, want single row == %s", positions, fold)
	}
}
