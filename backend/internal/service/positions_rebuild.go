package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// RebuildPositionsResult summarizes a regeneration pass.
type RebuildPositionsResult struct {
	// Pairs is the number of (account, asset) keys with transactions.
	Pairs int
	// Updated is the number of position rows upserted.
	Updated int
}

// RebuildPositions regenerates positions from the transaction ledger for one
// user, demonstrating the ADR-0017 invariant that positions is a pure fold of
// transactions: positions.quantity == Σ non-deleted transactions.amount per
// (account, asset). For every such pair it recomputes quantity (+ average-cost
// basis via valuation.Recompute) and upserts the position. Safe to re-run.
//
// When a trade's cost basis can't be priced (no trade-date FX), the pair still
// gets its quantity materialized from the raw fold with cost basis left
// unknown — the quantity invariant must hold even when valuation can't.
//
// The whole pass runs in one transaction so positions never observe a
// half-rebuilt state.
func RebuildPositions(
	ctx context.Context,
	db *gorm.DB,
	prices repository.PriceRepository,
	users repository.UserRepository,
	userID int64,
) (RebuildPositionsResult, error) {
	user, err := users.GetByID(ctx, userID)
	if err != nil {
		return RebuildPositionsResult{}, fmt.Errorf("rebuild positions: load user: %w", err)
	}

	var res RebuildPositionsResult
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		posRepo := repository.NewPositionRepository(tx)

		pairs, err := txRepo.DistinctAccountAssetPairs(ctx, userID)
		if err != nil {
			return fmt.Errorf("rebuild positions: list pairs: %w", err)
		}
		res.Pairs = len(pairs)

		for _, p := range pairs {
			rc, err := valuation.Recompute(ctx, txRepo, prices, userID, p.AccountID, p.AssetID, user.PrimaryCurrencyAssetID)
			if err != nil {
				if !errors.Is(err, valuation.ErrFXUnavailable) {
					return fmt.Errorf("rebuild positions: recompute (acct=%d asset=%d): %w", p.AccountID, p.AssetID, err)
				}
				// Cost basis needs an FX rate we don't have. Fall back to the
				// raw fold so quantity is still correct; cost basis stays NULL.
				qty, ferr := txRepo.FoldQuantity(ctx, userID, p.AccountID, p.AssetID)
				if ferr != nil {
					return fmt.Errorf("rebuild positions: fold fallback (acct=%d asset=%d): %w", p.AccountID, p.AssetID, ferr)
				}
				rc = valuation.RecomputeResult{Quantity: qty}
			}

			pos := &model.Position{
				UserID:    userID,
				AccountID: p.AccountID,
				AssetID:   p.AssetID,
				Quantity:  rc.Quantity,
			}
			if rc.HasCostBasis {
				cb := rc.CostBasis
				pos.CostBasis = &cb
			}
			if err := posRepo.Upsert(ctx, pos); err != nil {
				return fmt.Errorf("rebuild positions: upsert (acct=%d asset=%d): %w", p.AccountID, p.AssetID, err)
			}
			res.Updated++
		}
		return nil
	})
	if err != nil {
		return RebuildPositionsResult{}, err
	}
	return res, nil
}
