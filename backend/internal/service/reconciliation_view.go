package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// ReconciliationCheckpoint is one row in the per-account reconciliation
// history (#370): an observed quantity, the ledger fold immediately before
// it was applied, and — when a delta existed — the opening_balance/
// adjustment transaction that absorbed it.
type ReconciliationCheckpoint struct {
	ObservationID    int64           `json:"observation_id"`
	AssetID          int64           `json:"asset_id"`
	AsOf             time.Time       `json:"as_of"`
	Source           string          `json:"source"`
	ObservedQuantity decimal.Decimal `json:"observed_quantity"`
	// PriorFold is the transaction-ledger fold for (account, asset)
	// immediately before this checkpoint's reconciling row (if any) was
	// applied — Σ amount strictly before it in (transaction_date, id) order.
	PriorFold decimal.Decimal `json:"prior_fold"`
	// Delta is ObservedQuantity - PriorFold: zero when the fold already
	// matched the observation and no reconciling row was written.
	Delta decimal.Decimal `json:"delta"`
	// TransactionID/Kind are nil/"" when Delta is zero (no reconciling row
	// exists for this checkpoint).
	TransactionID    *int64     `json:"transaction_id,omitempty"`
	Kind             string     `json:"kind,omitempty"`
	Acknowledged     bool       `json:"acknowledged"`
	AcknowledgedAt   *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedNote *string    `json:"acknowledged_note,omitempty"`
	// NeedsAttention is true for an unacknowledged adjustment whose delta is
	// large relative to PriorFold — see Config.ReconciliationFlagPercentThreshold.
	// Always false for opening_balance (the expected day-0 anchor, never
	// "unexplained") and for already-acknowledged adjustments.
	NeedsAttention bool `json:"needs_attention"`
}

// ReconciliationReport is the per-account reconciliation view (#370):
// observation history, fold-vs-reported at each checkpoint, and whether the
// account has any unacknowledged unexplained delta.
type ReconciliationReport struct {
	AccountID      int64                      `json:"account_id"`
	Checkpoints    []ReconciliationCheckpoint `json:"checkpoints"`
	NeedsAttention bool                       `json:"needs_attention"`
}

// ReconciliationService builds the read-only reconciliation view and handles
// adjustment acknowledgment. It depends only on transaction + balance-
// observation repos — same PII-free, pii_repo-free shape as every other
// service outside pii_service (see docs/ARCHITECTURE.md § PII Isolation).
type ReconciliationService struct {
	txRepo           repository.TransactionRepository
	obsRepo          repository.BalanceObservationRepository
	acctRepo         repository.AccountRepository
	flagPctThreshold decimal.Decimal
}

func NewReconciliationService(
	txRepo repository.TransactionRepository,
	obsRepo repository.BalanceObservationRepository,
	acctRepo repository.AccountRepository,
	flagPercentThreshold float64,
) *ReconciliationService {
	return &ReconciliationService{
		txRepo:           txRepo,
		obsRepo:          obsRepo,
		acctRepo:         acctRepo,
		flagPctThreshold: decimal.NewFromFloat(flagPercentThreshold),
	}
}

// Report builds the reconciliation view for one account, newest checkpoint
// first (mirrors the observation repo's existing "newest first" convention).
func (s *ReconciliationService) Report(ctx context.Context, userID, accountID int64) (*ReconciliationReport, error) {
	if _, err := s.acctRepo.GetByID(ctx, userID, accountID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	observations, err := s.obsRepo.ListByAccount(ctx, userID, accountID)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	reconciling, err := s.txRepo.ListReconciling(ctx, userID, accountID)
	if err != nil {
		return nil, fmt.Errorf("list reconciling transactions: %w", err)
	}

	// Index reconciling rows by their causing observation (explicit link,
	// written at reconcile time — see ReconcilePosition). A second pass
	// below falls back to (asset, date) matching for legacy rows written
	// before the link existed.
	byObservation := make(map[int64]*model.Transaction, len(reconciling))
	usedByDate := make(map[string]*model.Transaction, len(reconciling))
	for i := range reconciling {
		t := &reconciling[i]
		if t.CausedByObservationID != nil {
			byObservation[*t.CausedByObservationID] = t
		}
		usedByDate[fmt.Sprintf("%d|%s", t.AssetID, t.TransactionDate.Format("2006-01-02"))] = t
	}

	checkpoints := make([]ReconciliationCheckpoint, 0, len(observations))
	anyFlagged := false
	for _, o := range observations {
		var tx *model.Transaction
		if t, ok := byObservation[o.ID]; ok {
			tx = t
		} else if t, ok := usedByDate[fmt.Sprintf("%d|%s", o.AssetID, o.AsOf.Format("2006-01-02"))]; ok {
			tx = t
		}

		cp := ReconciliationCheckpoint{
			ObservationID:    o.ID,
			AssetID:          o.AssetID,
			AsOf:             o.AsOf,
			Source:           o.Source,
			ObservedQuantity: o.ObservedQuantity,
			PriorFold:        o.ObservedQuantity,
			Acknowledged:     true,
		}

		if tx != nil {
			priorFold, err := s.txRepo.FoldQuantityBefore(ctx, userID, accountID, tx.AssetID, tx.TransactionDate, tx.ID)
			if err != nil {
				return nil, fmt.Errorf("fold before checkpoint: %w", err)
			}
			cp.PriorFold = priorFold
			cp.Delta = tx.Amount
			id := tx.ID
			cp.TransactionID = &id
			cp.Kind = tx.Kind
			cp.Acknowledged = tx.AcknowledgedAt != nil
			cp.AcknowledgedAt = tx.AcknowledgedAt
			cp.AcknowledgedNote = tx.AcknowledgedNote

			if tx.Kind == model.KindAdjustment && !cp.Acknowledged && exceedsReconciliationThreshold(cp.Delta, priorFold, s.flagPctThreshold) {
				cp.NeedsAttention = true
				anyFlagged = true
			}
		}

		checkpoints = append(checkpoints, cp)
	}

	return &ReconciliationReport{
		AccountID:      accountID,
		Checkpoints:    checkpoints,
		NeedsAttention: anyFlagged,
	}, nil
}

// Acknowledge marks an adjustment transaction reviewed, clearing its "needs
// attention" flag without altering ledger fields.
func (s *ReconciliationService) Acknowledge(ctx context.Context, userID, transactionID int64, note *string) (*model.Transaction, error) {
	tx, err := s.txRepo.AcknowledgeAdjustment(ctx, userID, transactionID, note)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	return tx, nil
}

// AccountsNeedingAttention returns the set of account IDs (for userID) that
// hold at least one unacknowledged adjustment exceeding the configured
// threshold — the bulk form AccountService uses to flag the Accounts/Insights
// list without a per-account round trip.
func (s *ReconciliationService) AccountsNeedingAttention(ctx context.Context, userID int64) (map[int64]bool, error) {
	adjustments, err := s.txRepo.ListUnacknowledgedAdjustments(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list unacknowledged adjustments: %w", err)
	}
	flagged := make(map[int64]bool)
	for i := range adjustments {
		t := &adjustments[i]
		priorFold, err := s.txRepo.FoldQuantityBefore(ctx, t.UserID, t.AccountID, t.AssetID, t.TransactionDate, t.ID)
		if err != nil {
			return nil, fmt.Errorf("fold before adjustment %d: %w", t.ID, err)
		}
		if exceedsReconciliationThreshold(t.Amount, priorFold, s.flagPctThreshold) {
			flagged[t.AccountID] = true
		}
	}
	return flagged, nil
}

// exceedsReconciliationThreshold reports whether an adjustment delta is
// "unexplained" relative to the ledger fold immediately before it: at or
// above pct of abs(priorFold), or any nonzero delta when priorFold is zero
// (an adjustment against nothing held is 100% unexplained by definition).
func exceedsReconciliationThreshold(delta, priorFold, pct decimal.Decimal) bool {
	if delta.IsZero() {
		return false
	}
	base := priorFold.Abs()
	if base.IsZero() {
		return true
	}
	return delta.Abs().Div(base).GreaterThanOrEqual(pct)
}
