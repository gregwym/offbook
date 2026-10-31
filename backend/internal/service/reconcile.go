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
// A backdated report (asOf before today) is compared with the ledger as of
// that day and never moves a balance a later anchor pinned; see
// reconcileFold and offsetAtNextAnchor (#669).
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
	return reconcilePositionForJob(ctx, txRepo, obsRepo, userID, accountID, assetID, reported, asOf, source, nil, nil)
}

// insertedRow is a ledger row the caller added to the (account, asset)
// immediately before reconciling, e.g. a statement's new flows. A backdated
// reconcile undoes their effect on balances a later anchor pinned (#680).
type insertedRow struct {
	date   time.Time
	amount decimal.Decimal
}

// reconcilePositionForJob is ReconcilePosition that links the observation and
// any adjustment it writes to a statement import job, so Undo can reverse
// them with the rows (#483). inserted lists the rows the caller just added
// for this (account, asset); see offsetAtNextAnchor.
func reconcilePositionForJob(
	ctx context.Context,
	txRepo repository.TransactionRepository,
	obsRepo repository.BalanceObservationRepository,
	userID, accountID, assetID int64,
	reported decimal.Decimal,
	asOf time.Time,
	source string,
	jobID *int64,
	inserted []insertedRow,
) (*model.Transaction, error) {
	obs := &model.AccountBalanceObservation{
		UserID:           userID,
		AccountID:        accountID,
		AssetID:          assetID,
		ObservedQuantity: reported,
		AsOf:             asOf,
		Source:           source,
		ImportJobID:      jobID,
	}
	if err := obsRepo.Insert(ctx, obs); err != nil {
		return nil, fmt.Errorf("reconcile: record observation: %w", err)
	}

	fold, historical, err := reconcileFold(ctx, txRepo, userID, accountID, assetID, asOf)
	if err != nil {
		return nil, err
	}
	delta := reported.Sub(fold)
	if delta.IsZero() {
		// A statement can match on its own day yet carry new rows that moved
		// a balance a later anchor pinned (#680).
		if historical {
			if err := offsetAtNextAnchor(ctx, txRepo, obsRepo, userID, accountID, assetID, delta, inserted, asOf, obs.ID, jobID); err != nil {
				return nil, err
			}
		}
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
		UserID:                userID,
		AccountID:             accountID,
		AssetID:               assetID,
		Kind:                  kind,
		Amount:                delta,
		Description:           &desc,
		TransactionDate:       asOf,
		Source:                "system",
		CausedByObservationID: &obs.ID,
		ImportJobID:           jobID,
	}
	if err := txRepo.Create(ctx, recTx); err != nil {
		return nil, fmt.Errorf("reconcile: write %s: %w", kind, err)
	}
	if historical {
		if err := offsetAtNextAnchor(ctx, txRepo, obsRepo, userID, accountID, assetID, delta, inserted, asOf, obs.ID, jobID); err != nil {
			return nil, err
		}
	}
	return recTx, nil
}

// reconcileFold returns the ledger fold a value reported asOf is compared
// with. A current report (asOf today or later) is compared with the whole
// ledger. A backdated one (#669) is compared with the ledger as it stood at
// the end of asOf's day, so flows dated after it stay where they are instead
// of being cancelled by the adjustment.
func reconcileFold(ctx context.Context, txRepo repository.TransactionRepository, userID, accountID, assetID int64, asOf time.Time) (decimal.Decimal, bool, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !asOf.UTC().Before(today) {
		fold, err := txRepo.FoldQuantity(ctx, userID, accountID, assetID)
		if err != nil {
			return decimal.Zero, false, fmt.Errorf("reconcile: fold: %w", err)
		}
		return fold, false, nil
	}
	fold, err := txRepo.FoldQuantityThrough(ctx, userID, accountID, assetID, asOf)
	if err != nil {
		return decimal.Zero, true, fmt.Errorf("reconcile: fold through %s: %w", asOf.Format("2006-01-02"), err)
	}
	return fold, true, nil
}

// offsetAtNextAnchor keeps a backdated correction from moving balances that a
// later anchor already pinned. When an observation or opening_balance/
// adjustment row exists on a later day, the balance from that day on was
// already reconciled, so the correction's delta is reversed there: the
// ledger then matches both the backdated report and the later one, and the
// current balance is unchanged. With no later anchor the delta carries
// forward, since nothing after asOf contradicts it.
//
// Rows the caller inserted on or before that later day moved its balance
// too, so the offset reverses them as well (#680). Without that, an older
// statement's new flows would shift a later confirmed balance. Only this
// operation's changes are reversed: an unrelated discrepancy at the later
// anchor is left for its own reconciliation to show.
func offsetAtNextAnchor(
	ctx context.Context,
	txRepo repository.TransactionRepository,
	obsRepo repository.BalanceObservationRepository,
	userID, accountID, assetID int64,
	delta decimal.Decimal,
	inserted []insertedRow,
	asOf time.Time,
	obsID int64,
	jobID *int64,
) error {
	next, err := obsRepo.EarliestAfter(ctx, userID, accountID, assetID, asOf)
	if err != nil {
		return fmt.Errorf("reconcile: later observation: %w", err)
	}
	rec, err := txRepo.EarliestReconcilingAfter(ctx, userID, accountID, assetID, asOf)
	if err != nil {
		return fmt.Errorf("reconcile: later anchor: %w", err)
	}
	if rec != nil && (next == nil || rec.Before(*next)) {
		next = rec
	}
	if next == nil {
		return nil
	}
	nextDay := next.UTC().Truncate(24 * time.Hour)
	shift := delta
	for _, r := range inserted {
		if !r.date.UTC().Truncate(24 * time.Hour).After(nextDay) {
			shift = shift.Add(r.amount)
		}
	}
	if shift.IsZero() {
		return nil
	}
	desc := "Reconciliation adjustment (offsets an earlier balance correction)"
	offset := &model.Transaction{
		UserID:                userID,
		AccountID:             accountID,
		AssetID:               assetID,
		Kind:                  model.KindAdjustment,
		Amount:                shift.Neg(),
		Description:           &desc,
		TransactionDate:       nextDay,
		Source:                "system",
		CausedByObservationID: &obsID,
		ImportJobID:           jobID,
	}
	if err := txRepo.Create(ctx, offset); err != nil {
		return fmt.Errorf("reconcile: write offset: %w", err)
	}
	return nil
}
