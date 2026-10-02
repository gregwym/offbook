package prices

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
)

// alphaVantageBaseURL is the public API root. Overridable for tests.
const alphaVantageBaseURL = "https://www.alphavantage.co"

// AlphaVantage is the keyed, opt-in equity/ETF/fund provider (#372,
// ADR-0014 equity addendum) — a reliability fallback behind Stooq's free
// feed, active only when an API key is configured (ALPHA_VANTAGE_API_KEY).
// GLOBAL_QUOTE serves one symbol per call, and the free tier is heavily
// rate-limited, so Pause paces consecutive calls within one Fetch. A
// rate-limited or unknown symbol degrades to skipped, never an error —
// the batch must survive one bad/throttled symbol.
type AlphaVantage struct {
	baseURL string
	apiKey  string
	client  *http.Client
	now     func() time.Time
	pause   time.Duration
}

// NewAlphaVantage returns a provider using the given API key. Construct it
// only when the key is non-empty — an empty key would 200 with an
// "Information" error for every symbol, i.e. the provider would be live but
// useless.
func NewAlphaVantage(apiKey string) *AlphaVantage {
	return &AlphaVantage{
		baseURL: alphaVantageBaseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 15 * time.Second},
		now:     time.Now,
		// Free tier is ~5 requests/minute; 12s between calls stays under it.
		pause: 12 * time.Second,
	}
}

// WithBaseURL points the provider at a different API root (tests).
func (a *AlphaVantage) WithBaseURL(u string) *AlphaVantage {
	a.baseURL = u
	return a
}

// WithPause overrides the between-symbol pause (tests; production callers
// should leave the free-tier-safe default alone).
func (a *AlphaVantage) WithPause(d time.Duration) *AlphaVantage {
	a.pause = d
	return a
}

func (a *AlphaVantage) Name() string { return "alphavantage" }

// Supports: equities and funds, same US-only assumption as Stooq — bare
// tickers resolve against the primary US listing.
func (a *AlphaVantage) Supports(asset model.Asset) bool {
	return asset.Kind == model.AssetKindEquity || asset.Kind == model.AssetKindFund
}

func (a *AlphaVantage) Fetch(ctx context.Context, assets []model.Asset, quote model.Asset) ([]Quote, error) {
	if len(assets) == 0 {
		return nil, nil
	}
	// GLOBAL_QUOTE prices in the listing's native currency; this provider
	// only covers the US-listed symbol set, so require a USD quote.
	if quote.Kind != model.AssetKindFiat || !strings.EqualFold(quote.Symbol, "USD") {
		return nil, nil
	}

	out := make([]Quote, 0, len(assets))
	for i, asset := range assets {
		if i > 0 && a.pause > 0 {
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(a.pause):
			}
		}
		q, ok, err := a.fetchOne(ctx, asset.Symbol)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		q.AssetID = asset.ID
		q.QuoteAssetID = quote.ID
		out = append(out, q)
	}
	return out, nil
}

// fetchOne returns the latest quote for one symbol. ok=false means the
// upstream has no usable quote right now (unknown symbol, rate limit,
// invalid key) — a coverage/availability gap, not a transport failure, so
// it degrades to skipped rather than aborting the batch. err is reserved
// for actual transport/protocol failures.
func (a *AlphaVantage) fetchOne(ctx context.Context, symbol string) (Quote, bool, error) {
	u := fmt.Sprintf("%s/query?function=GLOBAL_QUOTE&symbol=%s&apikey=%s",
		a.baseURL, url.QueryEscape(strings.ToUpper(symbol)), url.QueryEscape(a.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Quote{}, false, fmt.Errorf("alphavantage: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return Quote{}, false, fmt.Errorf("alphavantage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Quote{}, false, fmt.Errorf("alphavantage: unexpected status %d for %s", resp.StatusCode, symbol)
	}

	// Every GLOBAL_QUOTE field, numeric or not, arrives as a JSON string.
	var body struct {
		GlobalQuote map[string]string `json:"Global Quote"`
		Note        string            `json:"Note"`        // rate limit
		Information string            `json:"Information"` // bad key / other error
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Quote{}, false, fmt.Errorf("alphavantage: decode response: %w", err)
	}
	if body.Note != "" || body.Information != "" || len(body.GlobalQuote) == 0 {
		return Quote{}, false, nil
	}
	raw, ok := body.GlobalQuote["05. price"]
	if !ok || raw == "" {
		return Quote{}, false, nil
	}
	price, err := decimal.NewFromString(raw)
	if err != nil {
		return Quote{}, false, fmt.Errorf("alphavantage: bad price %q for %s: %w", raw, symbol, err)
	}
	return Quote{Price: price, AsOf: a.now().UTC()}, true, nil
}
