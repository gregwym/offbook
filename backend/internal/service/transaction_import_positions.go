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
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// Statement positions + closing balance (#375). The extractor proposes; the
// ingestion validators admit; commit routes every admitted quantity through
// ReconcilePosition — the same trust boundary and idempotency as Plaid-reported
// holdings (ADR-0017): a re-import whose fold already equals the reported
// value writes nothing. The app never invents a trade to explain a delta.

// AssetStatus tells the preview whether an extracted symbol maps to an
// existing asset or will be created on commit (user-visible, never silent).
type AssetStatus string

const (
	AssetExisting AssetStatus = "existing"
	AssetNew      AssetStatus = "new"
)

// ReconcileEffect is the ledger row a commit would write for a reported value.
type ReconcileEffect struct {
	// CurrentQuantity is the ledger fold the reported value is compared with
	// (for the cash asset it already includes this statement's new rows).
	CurrentQuantity decimal.Decimal `json:"current_quantity"`
	// Adjustment is reported − CurrentQuantity; zero means no row is written.
	Adjustment decimal.Decimal `json:"adjustment"`
	// Kind is "opening_balance" (first reconciling row for the asset),
	// "adjustment", or "" when Adjustment is zero.
	Kind string `json:"kind"`
}

// PositionPreview is one extracted holding as shown before commit.
type PositionPreview struct {
	Line        int              `json:"line"`
	Symbol      string           `json:"symbol"`
	Name        string           `json:"name,omitempty"`
	AssetKind   string           `json:"asset_kind"`
	Quantity    decimal.Decimal  `json:"quantity"`
	Price       *decimal.Decimal `json:"price,omitempty"`
	Value       *decimal.Decimal `json:"value,omitempty"`
	AsOf        time.Time        `json:"as_of"`
	Confidence  float64          `json:"confidence"`
	NeedsReview bool             `json:"needs_review"`
	Error       string           `json:"error,omitempty"`
	AssetStatus AssetStatus      `json:"asset_status,omitempty"`
	Effect      *ReconcileEffect `json:"effect,omitempty"`
}

// BalancePreview is the statement closing balance and its resulting effect on
// the account's cash asset.
type BalancePreview struct {
	Symbol   string          `json:"symbol"`
	Reported decimal.Decimal `json:"reported"`
	AsOf     time.Time       `json:"as_of"`
	Effect   ReconcileEffect `json:"effect"`
}

// WithStatementPositions wires what statement position/balance reconciliation
// needs: the DB handle (reconciliation + position upsert commit atomically)
// and the global assets table. Without it, extracted positions are ignored.
func (s *TransactionService) WithStatementPositions(db *gorm.DB, assets repository.AssetRepository) *TransactionService {
	s.db, s.assetRepo = db, assets
	return s
}

var positionAssetKinds = []string{
	model.AssetKindEquity, model.AssetKindFund, model.AssetKindCrypto,
	model.AssetKindBond, model.AssetKindOther, model.AssetKindCommodity,
}

// resolveAsset finds an existing non-fiat asset by symbol, preferring the
// extractor's proposed kind.
func (s *TransactionService) resolveAsset(ctx context.Context, symbol, proposedKind string) (*model.Asset, error) {
	kinds := append([]string{proposedKind}, positionAssetKinds...)
	for _, k := range kinds {
		a, err := s.assetRepo.GetBySymbolKind(ctx, symbol, k)
		if err == nil {
			return a, nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

// effectFor computes what ReconcilePosition would write against fold.
func (s *TransactionService) effectFor(ctx context.Context, userID, accountID, assetID int64, fold, reported decimal.Decimal) (ReconcileEffect, error) {
	e := ReconcileEffect{CurrentQuantity: fold, Adjustment: reported.Sub(fold)}
	if e.Adjustment.IsZero() {
		return e, nil
	}
	has, err := s.repo.HasReconcilingTxn(ctx, userID, accountID, assetID)
	if err != nil {
		return e, err
	}
	e.Kind = model.KindAdjustment
	if !has {
		e.Kind = model.KindOpeningBalance
	}
	return e, nil
}

// previewPositions fills res.Positions / res.ClosingBalance without writing.
func (s *TransactionService) previewPositions(ctx context.Context, userID int64, account *model.Account, ext *ingestion.Extraction, newRowSum decimal.Decimal, res *ImportResult) error {
	if s.assetRepo == nil {
		return nil
	}
	for _, p := range ext.Positions {
		pv := PositionPreview{
			Line: p.Line, Symbol: p.Symbol, Name: p.Name, AssetKind: p.AssetKind,
			Quantity: p.Quantity, Price: p.Price, Value: p.Value, AsOf: p.AsOf,
			Confidence: p.Confidence, Error: p.Err,
		}
		if p.Valid() {
			asset, err := s.resolveAsset(ctx, p.Symbol, p.AssetKind)
			if err != nil {
				return fmt.Errorf("resolve asset %s: %w", p.Symbol, err)
			}
			fold, assetID := decimal.Zero, int64(0)
			pv.AssetStatus = AssetNew
			if asset != nil {
				pv.AssetStatus, assetID = AssetExisting, asset.ID
				if fold, _, err = reconcileFold(ctx, s.repo, userID, account.ID, assetID, p.AsOf); err != nil {
					return fmt.Errorf("fold %s: %w", p.Symbol, err)
				}
			}
			eff := ReconcileEffect{CurrentQuantity: fold, Adjustment: p.Quantity.Sub(fold)}
			if !eff.Adjustment.IsZero() {
				eff.Kind = model.KindOpeningBalance
				if assetID != 0 {
					if eff, err = s.effectFor(ctx, userID, account.ID, assetID, fold, p.Quantity); err != nil {
						return err
					}
				}
			}
			pv.Effect = &eff
			if p.Confidence < reviewConfidenceThreshold {
				pv.NeedsReview = true
				res.ReviewCount++
			}
		}
		res.Positions = append(res.Positions, pv)
	}
	if ext.ClosingBalance != nil && ext.ClosingBalanceAt != nil {
		cash, err := s.assetRepo.GetByID(ctx, account.PrimaryQuoteAssetID)
		if err != nil {
			return fmt.Errorf("cash asset: %w", err)
		}
		// The closing balance is already the signed account value (negative
		// when owed, positive when a card or loan is in credit) — the extractor
		// contract — so it is the position quantity as-is (#670).
		reported := *ext.ClosingBalance
		fold, _, err := reconcileFold(ctx, s.repo, userID, account.ID, cash.ID, *ext.ClosingBalanceAt)
		if err != nil {
			return err
		}
		// This statement's new rows land in the cash fold before the balance
		// is reconciled, so the preview must include them.
		eff, err := s.effectFor(ctx, userID, account.ID, cash.ID, fold.Add(newRowSum), reported)
		if err != nil {
			return err
		}
		res.ClosingBalance = &BalancePreview{Symbol: cash.Symbol, Reported: reported, AsOf: *ext.ClosingBalanceAt, Effect: eff}
	}
	return nil
}

// commitPositions reconciles every valid extracted position and the closing
// balance in one DB transaction, then upserts positions.quantity so
// positions == Σ transactions per asset. Runs after the flow rows are inserted.
func (s *TransactionService) commitPositions(ctx context.Context, userID int64, account *model.Account, ext *ingestion.Extraction, res *ImportResult, jobID *int64) error {
	if s.assetRepo == nil || s.db == nil {
		return nil
	}
	type target struct {
		assetID  int64
		quantity decimal.Decimal
		asOf     time.Time
	}
	var targets []target
	for _, p := range ext.Positions {
		if !p.Valid() {
			continue
		}
		asset, err := s.resolveAsset(ctx, p.Symbol, p.AssetKind)
		if err != nil {
			return fmt.Errorf("resolve asset %s: %w", p.Symbol, err)
		}
		if asset == nil {
			display := p.Name
			if display == "" {
				display = p.Symbol
			}
			if asset, err = s.assetRepo.EnsureBySymbolKind(ctx, p.Symbol, p.AssetKind, display); err != nil {
				return fmt.Errorf("ensure asset %s: %w", p.Symbol, err)
			}
		}
		targets = append(targets, target{asset.ID, p.Quantity, p.AsOf})
	}
	if ext.ClosingBalance != nil && ext.ClosingBalanceAt != nil {
		targets = append(targets, target{
			account.PrimaryQuoteAssetID,
			*ext.ClosingBalance, // signed account value, as in previewPositions
			*ext.ClosingBalanceAt,
		})
	}
	if len(targets) == 0 {
		return nil
	}
	// The statement's new flows are already in the cash ledger. A backdated
	// reconcile must not let them move a balance a later anchor pinned (#680).
	var inserted []insertedRow
	for _, r := range res.Rows {
		if r.Status == ImportNew {
			inserted = append(inserted, insertedRow{date: r.Date, amount: r.Amount})
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		obsRepo := repository.NewBalanceObservationRepository(tx)
		posRepo := repository.NewPositionRepository(tx)
		for _, t := range targets {
			var rows []insertedRow
			if t.assetID == account.PrimaryQuoteAssetID {
				rows = inserted
			}
			rec, err := reconcilePositionForJob(ctx, txRepo, obsRepo, userID, account.ID, t.assetID, t.quantity, t.asOf, "statement", jobID, rows)
			if err != nil {
				return err
			}
			if rec != nil {
				res.AdjustmentsWritten++
			}
			// The position is the whole ledger, not the reported value: an
			// older statement's balance must not reset today's (#669).
			qty, err := txRepo.FoldQuantity(ctx, userID, account.ID, t.assetID)
			if err != nil {
				return fmt.Errorf("fold position: %w", err)
			}
			if err := posRepo.Upsert(ctx, &model.Position{
				UserID: userID, AccountID: account.ID, AssetID: t.assetID, Quantity: qty,
			}); err != nil {
				return fmt.Errorf("upsert position: %w", err)
			}
		}
		res.PositionsReconciled = len(targets)
		return nil
	})
}
