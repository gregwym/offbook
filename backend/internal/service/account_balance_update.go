package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

var (
	// ErrAccountPlaidManaged is returned when a balance update targets a
	// Plaid-linked account: sync owns its balance observations.
	ErrAccountPlaidManaged = errors.New("this account's balance comes from its bank connection")
	// ErrBalanceAsOfInFuture rejects a balance dated after today.
	ErrBalanceAsOfInFuture = errors.New("as_of must not be in the future")
)

// UpdateBalanceInput is a user-reported balance for a manual account. Balance
// is signed the same way the account's balance is shown (liabilities
// negative). AsOf is a calendar date; nil means today.
type UpdateBalanceInput struct {
	Balance decimal.Decimal
	AsOf    *time.Time
}

// RecordedBalanceTxn is the ledger row a balance update wrote, if any.
type RecordedBalanceTxn struct {
	ID     int64           `json:"id"`
	Kind   string          `json:"kind"`
	Amount decimal.Decimal `json:"amount"`
}

// UpdateBalanceResult carries the refreshed account and what the update
// recorded. Recorded is nil when the ledger already matched the balance.
type UpdateBalanceResult struct {
	Account  *AccountResponse    `json:"account"`
	Recorded *RecordedBalanceTxn `json:"recorded"`
}

// UpdateBalance records a manual balance observation for a non-Plaid account
// and reconciles the ledger to it (#504): the first one writes an
// opening_balance anchor, later ones an adjustment linked to the observation,
// so the Reconciliation view explains the change. Observation, ledger row and
// position refresh share one DB transaction.
func (s *AccountService) UpdateBalance(ctx context.Context, userID, accountID int64, in UpdateBalanceInput) (*UpdateBalanceResult, error) {
	a, err := s.Get(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	if a.PlaidAccountID != nil || a.PlaidItemID != nil {
		return nil, ErrAccountPlaidManaged
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	asOf := today
	if in.AsOf != nil {
		asOf = time.Date(in.AsOf.Year(), in.AsOf.Month(), in.AsOf.Day(), 0, 0, 0, 0, time.UTC)
		// One day of slack: the user's local "today" can be ahead of UTC.
		if asOf.After(today.AddDate(0, 0, 1)) {
			return nil, ErrBalanceAsOfInFuture
		}
	}

	var recorded *RecordedBalanceTxn
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		rec, err := ReconcilePosition(ctx, txRepo, repository.NewBalanceObservationRepository(tx),
			userID, a.ID, a.PrimaryQuoteAssetID, in.Balance, asOf, "manual")
		if err != nil {
			return err
		}
		if rec != nil {
			recorded = &RecordedBalanceTxn{ID: rec.ID, Kind: rec.Kind, Amount: rec.Amount}
		}
		qty, err := txRepo.FoldQuantity(ctx, userID, a.ID, a.PrimaryQuoteAssetID)
		if err != nil {
			return fmt.Errorf("fold position: %w", err)
		}
		return repository.NewPositionRepository(tx).Upsert(ctx, &model.Position{
			UserID: userID, AccountID: a.ID, AssetID: a.PrimaryQuoteAssetID, Quantity: qty,
		})
	}); err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}

	resp, err := s.GetResponse(ctx, userID, a.ID)
	if err != nil {
		return nil, err
	}
	return &UpdateBalanceResult{Account: resp, Recorded: recorded}, nil
}
