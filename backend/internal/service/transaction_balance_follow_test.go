package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// TestTransactionService_ManualEditsMoveAccountBalance guards #446: a manual
// account whose balance is only ever fed by manually entered transactions must
// show ledger-derived balance (Σ flow), not 0.00, and stay correct through
// edit and delete.
func TestTransactionService_ManualEditsMoveAccountBalance(t *testing.T) {
	acctSvc, userID, g := newAccountSvc(t)
	acctSvc.WithValuation(valuation.NewService(
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewAssetRepository(g),
		repository.NewAccountRepository(g),
	))
	txSvc := service.NewTransactionService(
		repository.NewTransactionRepository(g),
		repository.NewAccountRepository(g),
		repository.NewCategoryRepository(g),
	).WithStatementPositions(g, repository.NewAssetRepository(g))
	ctx := context.Background()

	acct, err := acctSvc.Create(ctx, userID, validInput(tcSuffix(446)))
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	balance := func(t *testing.T, want string) {
		t.Helper()
		r, err := acctSvc.GetResponse(ctx, userID, acct.ID)
		if err != nil {
			t.Fatalf("get response: %v", err)
		}
		if !r.Balance.Equal(decimal.RequireFromString(want)) {
			t.Errorf("balance = %s, want %s", r.Balance, want)
		}
		if !r.BalanceComplete {
			t.Error("balance_complete = false, want true")
		}
	}
	add := func(amount string) int64 {
		t.Helper()
		tx, err := txSvc.Create(ctx, userID, service.CreateTransactionInput{
			AccountID:       acct.ID,
			Amount:          decimal.RequireFromString(amount),
			TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			Source:          "manual",
		})
		if err != nil {
			t.Fatalf("create %s: %v", amount, err)
		}
		return tx.ID
	}

	add("3000.00")
	id := add("-82.15")
	balance(t, "2917.85")

	newAmt := decimal.RequireFromString("-100.10")
	if _, err := txSvc.Update(ctx, userID, id, service.UpdateTransactionInput{Amount: &newAmt}); err != nil {
		t.Fatalf("update: %v", err)
	}
	balance(t, "2899.90")

	if err := txSvc.SoftDelete(ctx, userID, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	balance(t, "3000.00")
}

// TestTransactionService_ConcurrentCreatesKeepPositionInSync guards #593: two
// creates on one account each re-fold the ledger and upsert the position. If
// the fold and upsert aren't serialized, the slower writer can commit a fold
// that predates the other's row, and the account balance and headline net
// worth silently drop that row while the ledger (and the trend that folds it)
// still has it.
func TestTransactionService_ConcurrentCreatesKeepPositionInSync(t *testing.T) {
	acctSvc, userID, g := newAccountSvc(t)
	txSvc := service.NewTransactionService(
		repository.NewTransactionRepository(g),
		repository.NewAccountRepository(g),
		repository.NewCategoryRepository(g),
	).WithStatementPositions(g, repository.NewAssetRepository(g))
	ctx := context.Background()

	acct, err := acctSvc.Create(ctx, userID, validInput(tcSuffix(593)))
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	txRepo := repository.NewTransactionRepository(g)
	want, err := txRepo.FoldQuantity(ctx, userID, acct.ID, acct.PrimaryQuoteAssetID) // opening balance, if any
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	const n = 40
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		amt := decimal.New(int64(-100-i), -2) // -1.01 … -1.40, all distinct
		want = want.Add(amt)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := txSvc.Create(ctx, userID, service.CreateTransactionInput{
				AccountID:       acct.ID,
				Amount:          amt,
				TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
				Source:          "manual",
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	fold, err := txRepo.FoldQuantity(ctx, userID, acct.ID, acct.PrimaryQuoteAssetID)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !fold.Equal(want) {
		t.Fatalf("ledger fold = %s, want %s", fold, want)
	}
	positions, err := repository.NewPositionRepository(g).ListByAccountID(ctx, userID, acct.ID)
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].Quantity.Equal(want) {
		t.Errorf("position quantity = %s, want ledger fold %s (lost update)", positions[0].Quantity, want)
	}
}
