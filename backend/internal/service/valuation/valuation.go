// Package valuation centralizes the "positions × prices" math that
// powers every monetary read after ADR-0013. Per the ADR, quantities
// are facts and balances are derived — never stored on accounts. This
// package is the single place that walks the (asset, quote_asset,
// as_of) graph and decides when a price is too stale to trust.
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

// DefaultStaleWindow is how recently a price must have been observed
// for the helper to use it. Beyond this, callers get ErrStalePrice so
// the UI can render "stale" instead of silently showing 0 (or worse,
// a year-old price as if it were current).
const DefaultStaleWindow = 7 * 24 * time.Hour

// ErrStalePrice is returned when no price observation exists within
// the stale window for a requested (asset → quote) hop. Callers
// decide whether to surface as a warning or treat as 0.
var ErrStalePrice = errors.New("valuation: no fresh price within stale window")

// Service computes monetary values from positions × prices. It does
// one-hop FX through the asset's native quote currency when no direct
// (asset → user quote) price exists — e.g. AAPL is quoted in USD; to
// price it in EUR we walk AAPL→USD then USD→EUR.
type Service struct {
	positions repository.PositionRepository
	prices    repository.PriceRepository
	assets    repository.AssetRepository
	accounts  repository.AccountRepository

	staleWindow time.Duration
}

// NewService wires the repositories the helper reads from. None are
// optional — every method reads at least positions and prices.
func NewService(
	positions repository.PositionRepository,
	prices repository.PriceRepository,
	assets repository.AssetRepository,
	accounts repository.AccountRepository,
) *Service {
	return &Service{
		positions:   positions,
		prices:      prices,
		assets:      assets,
		accounts:    accounts,
		staleWindow: DefaultStaleWindow,
	}
}

// WithStaleWindow returns a copy of the service with the stale tolerance
// overridden. Pass a non-positive value to disable the staleness check
// entirely (used by the historical net-worth trend, where any-age price is
// fine). Copy semantics so a historical caller can't disable freshness for a
// shared instance that also serves current reads.
func (s *Service) WithStaleWindow(d time.Duration) *Service {
	cp := *s
	cp.staleWindow = d
	return &cp
}

// StaleWindow returns the currently-configured stale window. Useful
// for tests and for callers that want to render "as-of" labels.
func (s *Service) StaleWindow() time.Duration { return s.staleWindow }

// Value returns pos.Quantity priced in quoteAssetID at asOf. The
// graph walk:
//
//  1. If position.asset_id == quoteAssetID, return quantity directly
//     (no price lookup; never stale).
//  2. Try a direct price (asset_id → quoteAssetID).
//  3. If the position's asset has a native quote currency that is
//     neither the asset nor quoteAssetID, walk the two hops
//     asset → native quote → quoteAssetID.
//
// Returns ErrStalePrice if no fresh price chain is found. The
// returned error wraps ErrStalePrice so errors.Is callers work.
func (s *Service) Value(ctx context.Context, pos model.Position, asOf time.Time, quoteAssetID int64) (decimal.Decimal, error) {
	if pos.AssetID == quoteAssetID {
		return pos.Quantity, nil
	}

	if rate, ok, err := s.lookupRate(ctx, pos.AssetID, quoteAssetID, asOf); err != nil {
		return decimal.Zero, err
	} else if ok {
		return pos.Quantity.Mul(rate), nil
	}

	asset, err := s.assets.GetByID(ctx, pos.AssetID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("valuation: load asset %d: %w", pos.AssetID, err)
	}
	if asset.QuoteCurrencyAssetID == nil ||
		*asset.QuoteCurrencyAssetID == pos.AssetID ||
		*asset.QuoteCurrencyAssetID == quoteAssetID {
		return decimal.Zero, fmt.Errorf("%w: asset %d → quote %d", ErrStalePrice, pos.AssetID, quoteAssetID)
	}

	leg1, ok, err := s.lookupRate(ctx, pos.AssetID, *asset.QuoteCurrencyAssetID, asOf)
	if err != nil {
		return decimal.Zero, err
	}
	if !ok {
		return decimal.Zero, fmt.Errorf("%w: asset %d → native quote %d", ErrStalePrice, pos.AssetID, *asset.QuoteCurrencyAssetID)
	}
	leg2, ok, err := s.lookupRate(ctx, *asset.QuoteCurrencyAssetID, quoteAssetID, asOf)
	if err != nil {
		return decimal.Zero, err
	}
	if !ok {
		return decimal.Zero, fmt.Errorf("%w: native quote %d → user quote %d", ErrStalePrice, *asset.QuoteCurrencyAssetID, quoteAssetID)
	}
	return pos.Quantity.Mul(leg1).Mul(leg2), nil
}

// SetValuation is the result of valuing a set of positions in a single quote
// currency at a point in time. It deliberately separates the priced total from
// the positions that could not be priced, so callers can render an
// "incomplete" figure rather than a wrong-but-confident one (#282): an unpriced
// position contributes nothing to Value and is instead surfaced in Unpriced.
//
// Complete() is true when every position priced. A zero Value with a non-empty
// Unpriced is "we don't know", distinct from a genuine zero balance.
type SetValuation struct {
	// Value is the sum of every position that priced fresh, in the quote asset.
	Value decimal.Decimal
	// Unpriced lists the asset_ids whose price chain was stale/missing at asOf.
	// A nil/empty slice means the total is complete.
	Unpriced []int64
}

// Complete reports whether every position in the set had a fresh price — i.e.
// Value is the whole story, not a partial sum.
func (v SetValuation) Complete() bool { return len(v.Unpriced) == 0 }

// ValuePositions values an arbitrary set of positions in quoteAssetID at asOf
// and reports which ones could not be priced. This is the single derivation
// behind net worth, allocation, and trend (#282): callers assemble the
// position set — current rows for "now", or synthetic rows carrying the
// historical fold quantity for a past asOf — and differ only in that set and
// the quote currency.
//
// Missing prices are NOT coerced to $0: a stale/absent price chain drops the
// position from Value and records its asset in Unpriced. Other errors abort.
func (s *Service) ValuePositions(ctx context.Context, positions []model.Position, asOf time.Time, quoteAssetID int64) (SetValuation, error) {
	out := SetValuation{Value: decimal.Zero}
	for _, p := range positions {
		v, err := s.Value(ctx, p, asOf, quoteAssetID)
		if errors.Is(err, ErrStalePrice) {
			out.Unpriced = append(out.Unpriced, p.AssetID)
			continue
		}
		if err != nil {
			return SetValuation{}, err
		}
		out.Value = out.Value.Add(v)
	}
	return out, nil
}

// SeriesPoint is one timestamped entry of a valuation series.
type SeriesPoint struct {
	AsOf time.Time
	SetValuation
}

// Series values a position set at each asOf and returns the aligned series.
// positionsAt supplies the set for a given instant — current rows for "now",
// or synthetic rows carrying the historical fold quantity for a past instant.
// This is the single trend derivation behind both personal and household net
// worth (#282): callers differ only in the positionsAt closure (which encodes
// scope + tenancy) and the quote currency, so the same account set yields the
// same series. Use WithStaleWindow(0) for historical points where any-age
// prices are acceptable.
func (s *Service) Series(
	ctx context.Context,
	asOfs []time.Time,
	quoteAssetID int64,
	positionsAt func(context.Context, time.Time) ([]model.Position, error),
) ([]SeriesPoint, error) {
	out := make([]SeriesPoint, 0, len(asOfs))
	for _, asOf := range asOfs {
		positions, err := positionsAt(ctx, asOf)
		if err != nil {
			return nil, err
		}
		val, err := s.ValuePositions(ctx, positions, asOf, quoteAssetID)
		if err != nil {
			return nil, err
		}
		out = append(out, SeriesPoint{AsOf: asOf, SetValuation: val})
	}
	return out, nil
}

// AccountBalance returns the sum of the account's positions valued at
// asOf in the account's primary_quote_asset_id. Positions whose price
// chain is stale contribute 0 — this matches the soft-fallback behavior
// of the dashboard query so callers can render a number without crashing,
// and a separate stale-price audit surface can warn the user.
//
// Stale positions are still reported in `staleAssets` so the caller can
// render the warning. An empty `staleAssets` means every position priced
// fresh. The caller is responsible for tenancy (passing the right userID).
func (s *Service) AccountBalance(ctx context.Context, userID, accountID int64, asOf time.Time) (decimal.Decimal, []int64, error) {
	acct, err := s.accounts.GetByID(ctx, userID, accountID)
	if err != nil {
		return decimal.Zero, nil, fmt.Errorf("valuation: load account %d: %w", accountID, err)
	}
	positions, err := s.positions.ListByAccountID(ctx, userID, accountID)
	if err != nil {
		return decimal.Zero, nil, fmt.Errorf("valuation: list positions for account %d: %w", accountID, err)
	}
	val, err := s.ValuePositions(ctx, positions, asOf, acct.PrimaryQuoteAssetID)
	if err != nil {
		return decimal.Zero, nil, err
	}
	return val.Value, val.Unpriced, nil
}

// lookupRate returns the most-recent price for (assetID → quoteAssetID)
// at-or-before asOf, gated by the configured stale window. Returns
// (price, true, nil) on hit, (_, false, nil) when no row or when the
// freshest row is older than the window, (_, _, err) on a real I/O
// error. A staleWindow <= 0 disables the freshness gate entirely.
func (s *Service) lookupRate(ctx context.Context, assetID, quoteAssetID int64, asOf time.Time) (decimal.Decimal, bool, error) {
	p, err := s.prices.LatestPriceAt(ctx, assetID, quoteAssetID, asOf)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return decimal.Zero, false, nil
		}
		return decimal.Zero, false, fmt.Errorf("valuation: price lookup %d → %d at %s: %w", assetID, quoteAssetID, asOf.Format(time.RFC3339), err)
	}
	if s.staleWindow > 0 && asOf.Sub(p.AsOf) > s.staleWindow {
		return decimal.Zero, false, nil
	}
	return p.Price, true, nil
}
