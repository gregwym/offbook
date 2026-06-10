// Package prices implements ADR-0014: pluggable price & FX providers that
// keep the `prices` time series current between imports. Providers only
// produce price observations — they never touch positions or transactions
// (quantity is ledger-derived per ADR-0017), and the only thing sent
// upstream is the symbol list, never quantities, amounts, or PII.
package prices

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
)

// Quote is one observed price: AssetID priced in QuoteAssetID at AsOf.
type Quote struct {
	AssetID      int64
	QuoteAssetID int64
	Price        decimal.Decimal
	AsOf         time.Time
}

// Provider fetches current prices from one upstream source. Implementations
// mirror the service/ai provider pattern: small, per-source, registered with
// the orchestrating Service.
type Provider interface {
	// Name identifies the provider and doubles as the prices.source value
	// ('coingecko', 'ecb', …) so every observation stays attributable.
	Name() string
	// Supports reports whether this provider can quote the asset at all.
	// The service uses it to partition held assets across providers.
	Supports(a model.Asset) bool
	// Fetch returns observations for the given (already Supports-filtered)
	// assets, priced in the quote asset. Assets the upstream can't quote —
	// unknown symbol, unsupported quote currency — are silently omitted
	// from the result; the caller reports them as skipped. An error means
	// the upstream call itself failed.
	Fetch(ctx context.Context, assets []model.Asset, quote model.Asset) ([]Quote, error)
}
