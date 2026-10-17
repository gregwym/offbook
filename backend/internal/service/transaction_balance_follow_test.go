package service_test

import (
	"context"
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
