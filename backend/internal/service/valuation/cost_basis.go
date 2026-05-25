package valuation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// ErrFXUnavailable is returned by Recompute when the trade-date FX rate
// needed to convert a cash leg into the user's primary currency isn't
// present in `prices`. Surface to the caller so they can fall back to
// "cost basis unknown" rather than booking a wrong number.
var ErrFXUnavailable = errors.New("cost basis: no FX rate for trade date")

// RecomputeResult is what Recompute returns for one (account × asset)
// after walking the full trade history. Quantity always reflects the
// sum of `transactions.amount` for the (account, asset) pair —
// canonical, never approximated. CostBasis is denominated in the
// user's primary currency.
//
// HasCostBasis reports whether any trade contributed to the running
// cost basis. If false, callers should store position.cost_basis as
// NULL — "unknown" is the honest answer when we have only transfer
// rows with no priced cash partner.
type RecomputeResult struct {
	Quantity     decimal.Decimal
	CostBasis    decimal.Decimal
	HasCostBasis bool
}

// Recompute walks every non-deleted transaction for (userID, accountID,
// assetID) in chronological order and re-derives quantity + average-
// cost basis from scratch. Pure-ish — reads only; never writes.
//
// Method (per ADR-0013 §4):
//   - Buy (security leg amount > 0, paired cash leg):
//     cost_basis += |cash_leg.amount| × fx(cash_asset → primary, trade_date)
//   - Sell (security leg amount < 0, paired cash leg):
//     cost_basis -= (cost_basis / quantity_before) × |sold_qty|
//   - Unpaired inflow (transfer in, dividend-as-shares): quantity++,
//     cost basis is unknown for the added units — leave running basis
//     untouched; the per-unit basis dilutes naturally on the next sell.
//   - Unpaired outflow (transfer out): proportional cost-basis removal,
//     same shape as a sell.
//
// FX uses prices.LatestPriceAt at the trade date with no stale-window
// gate — trade-date FX is the canonical lookup. If no price row exists
// for the (cash_asset → primary) pair at-or-before the trade date,
// Recompute returns ErrFXUnavailable. When cash_asset == primary the
// hop is skipped entirely.
//
// userID + assetID are required for tenant scoping; primaryAssetID is
// the user's primary_currency_asset_id (read once by the caller).
// txRepo, prices, and an asset-loader for partner lookups are injected.
func Recompute(
	ctx context.Context,
	txRepo repository.TransactionRepository,
	prices repository.PriceRepository,
	userID, accountID, assetID, primaryAssetID int64,
) (RecomputeResult, error) {
	if assetID == primaryAssetID {
		// Cash position in the primary currency — cost basis is the
		// running quantity itself, but no cost-basis concept applies.
		qty, err := sumQty(ctx, txRepo, userID, accountID, assetID)
		if err != nil {
			return RecomputeResult{}, err
		}
		return RecomputeResult{Quantity: qty}, nil
	}

	txns, err := txRepo.ListByAccountAndAsset(ctx, userID, accountID, assetID)
	if err != nil {
		return RecomputeResult{}, fmt.Errorf("cost basis: list txns: %w", err)
	}

	qty := decimal.Zero
	cb := decimal.Zero
	hasCB := false

	for _, t := range txns {
		if t.Amount.IsZero() {
			continue
		}
		// Paired trade — try to derive a priced cash leg.
		if t.TransferPairID != nil {
			cashAmtPrimary, hadCash, err := pairedCashInPrimary(ctx, txRepo, prices, t, primaryAssetID)
			if err != nil {
				return RecomputeResult{}, err
			}
			if hadCash {
				switch {
				case t.Amount.IsPositive():
					// Buy: add cash to cost basis, qty up.
					cb = cb.Add(cashAmtPrimary)
					qty = qty.Add(t.Amount)
					hasCB = true
				case t.Amount.IsNegative():
					// Sell: proportional cost-basis removal.
					if qty.IsPositive() {
						perUnit := cb.Div(qty)
						removed := perUnit.Mul(t.Amount.Abs())
						cb = cb.Sub(removed)
						if cb.IsNegative() {
							cb = decimal.Zero
						}
					}
					qty = qty.Add(t.Amount)
					hasCB = true
				}
				continue
			}
			// Paired with a non-cash row (e.g. share-for-share swap).
			// Fall through to the unpaired path: quantity only.
		}
		// Unpaired leg: quantity-only update with proportional cost-basis
		// removal on outflow (we know lots are leaving even without a cash
		// partner).
		if t.Amount.IsNegative() && qty.IsPositive() && hasCB {
			perUnit := cb.Div(qty)
			removed := perUnit.Mul(t.Amount.Abs())
			cb = cb.Sub(removed)
			if cb.IsNegative() {
				cb = decimal.Zero
			}
		}
		qty = qty.Add(t.Amount)
	}

	// Quantity bottomed out — reset basis. Otherwise rounding noise can
	// leave a few satoshi of basis sitting on a closed position.
	if qty.IsZero() {
		cb = decimal.Zero
	}

	return RecomputeResult{Quantity: qty, CostBasis: cb, HasCostBasis: hasCB}, nil
}

// pairedCashInPrimary loads `t`'s partner transaction and, if the
// partner is a same-account row in a fiat asset, returns |partner|
// converted to the user's primary currency at the trade date. Returns
// (_, false, nil) when the partner is not a recognizable cash leg —
// caller should treat as unpaired.
func pairedCashInPrimary(
	ctx context.Context,
	txRepo repository.TransactionRepository,
	prices repository.PriceRepository,
	t model.Transaction,
	primaryAssetID int64,
) (decimal.Decimal, bool, error) {
	if t.TransferPairID == nil {
		return decimal.Zero, false, nil
	}
	pair, err := txRepo.GetByID(ctx, t.UserID, *t.TransferPairID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return decimal.Zero, false, nil
		}
		return decimal.Zero, false, err
	}
	if pair.AccountID != t.AccountID || pair.AssetID == t.AssetID {
		return decimal.Zero, false, nil
	}
	amtAbs := pair.Amount.Abs()
	if pair.AssetID == primaryAssetID {
		return amtAbs, true, nil
	}
	rate, err := prices.LatestPriceAt(ctx, pair.AssetID, primaryAssetID, atEndOfDay(t.TransactionDate))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return decimal.Zero, false, fmt.Errorf("%w: %d → %d on %s",
				ErrFXUnavailable, pair.AssetID, primaryAssetID, t.TransactionDate.Format("2006-01-02"))
		}
		return decimal.Zero, false, fmt.Errorf("cost basis: fx lookup: %w", err)
	}
	return amtAbs.Mul(rate.Price), true, nil
}

// sumQty totals `transactions.amount` for the (user, account, asset)
// trio — the canonical quantity for a position. Used in the fast path
// when the asset equals the user's primary currency (cost basis is
// trivially "quantity" and we don't need the full walk).
func sumQty(ctx context.Context, txRepo repository.TransactionRepository, userID, accountID, assetID int64) (decimal.Decimal, error) {
	rows, err := txRepo.ListByAccountAndAsset(ctx, userID, accountID, assetID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("cost basis: list txns: %w", err)
	}
	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.Amount)
	}
	return total, nil
}

// atEndOfDay returns the supplied date with time set to 23:59:59 UTC.
// Price lookups use the most-recent price at-or-before this instant, so
// a trade dated 2026-05-15 picks up any FX observation written during
// that same calendar day (e.g. ECB's daily fix).
func atEndOfDay(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, time.UTC)
}
