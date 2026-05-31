package service

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// ReconcilePosition makes the transaction-ledger fold equal `reported` for one
// (account, asset) by writing a single reconciling transaction for any delta,
// and records the observation (ADR-0017). The first reconciling row for an
// (account, asset) is an opening_balance anchor; later ones are adjustments.
//
// Returns the written transaction, or nil when the fold already equals
// `reported` (no reconciling row needed). It is idempotent: a second call with
// the same reported value writes nothing.
//
// Callers pass repos bound to the surrounding DB transaction so the
// observation, fold read, and reconciling write commit atomically. `source`
// identifies who reported the value (e.g. "plaid") and is stored on the
// observation; the reconciling transaction itself is always source="system".
func ReconcilePosition(
	ctx context.Context,
	txRepo repository.TransactionRepository,
	obsRepo repository.BalanceObservationRepository,
	userID, accountID, assetID int64,
	reported decimal.Decimal,
	asOf time.Time,
	source string,
) (*model.Transaction, error) {
	if err := obsRepo.Insert(ctx, &model.AccountBalanceObservation{
		UserID:           userID,
		AccountID:        accountID,
		AssetID:          assetID,
		ObservedQuantity: reported,
		AsOf:             asOf,
		Source:           source,
	}); err != nil {
		return nil, fmt.Errorf("reconcile: record observation: %w", err)
	}

	fold, err := txRepo.FoldQuantity(ctx, userID, accountID, assetID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: fold: %w", err)
	}
	delta := reported.Sub(fold)
	if delta.IsZero() {
		return nil, nil
	}

	hasAnchor, err := txRepo.HasReconcilingTxn(ctx, userID, accountID, assetID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: anchor check: %w", err)
	}
	kind := model.KindAdjustment
	desc := "Reconciliation adjustment"
	if !hasAnchor {
		kind = model.KindOpeningBalance
		desc = "Opening balance"
	}
	recTx := &model.Transaction{
		UserID:          userID,
		AccountID:       accountID,
		AssetID:         assetID,
		Kind:            kind,
		Amount:          delta,
		Description:     &desc,
		TransactionDate: asOf,
		Source:          "system",
	}
	if err := txRepo.Create(ctx, recTx); err != nil {
		return nil, fmt.Errorf("reconcile: write %s: %w", kind, err)
	}
	return recTx, nil
}
