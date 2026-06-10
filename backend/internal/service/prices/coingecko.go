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

// coingeckoBaseURL is the public, keyless API root. Overridable for tests.
const coingeckoBaseURL = "https://api.coingecko.com/api/v3"

// coingeckoIDBySymbol maps asset symbols to CoinGecko coin IDs. Static and
// code-reviewed by design (ADR-0014 §5): runtime resolution via /coins/list
// is ambiguous (ticker collisions) and unreviewable. A held symbol missing
// here surfaces as "skipped" in the refresh result — extend with a PR.
var coingeckoIDBySymbol = map[string]string{
	"BTC":   "bitcoin",
	"ETH":   "ethereum",
	"USDT":  "tether",
	"USDC":  "usd-coin",
	"BNB":   "binancecoin",
	"XRP":   "ripple",
	"SOL":   "solana",
	"ADA":   "cardano",
	"DOGE":  "dogecoin",
	"DOT":   "polkadot",
	"MATIC": "matic-network",
	"LTC":   "litecoin",
	"AVAX":  "avalanche-2",
	"LINK":  "chainlink",
	"UNI":   "uniswap",
	"BCH":   "bitcoin-cash",
	"XLM":   "stellar",
	"ATOM":  "cosmos",
	"ETC":   "ethereum-classic",
	"FIL":   "filecoin",
}

// CoinGecko quotes crypto assets via the free /simple/price endpoint. No
// API key: the only egress is the coin-ID list and quote currency.
type CoinGecko struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

// NewCoinGecko returns a provider hitting the public CoinGecko API.
func NewCoinGecko() *CoinGecko {
	return &CoinGecko{
		baseURL: coingeckoBaseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
		now:     time.Now,
	}
}

// WithBaseURL points the provider at a different API root (tests).
func (c *CoinGecko) WithBaseURL(u string) *CoinGecko {
	c.baseURL = u
	return c
}

func (c *CoinGecko) Name() string { return "coingecko" }

// Supports: crypto assets whose symbol is in the static map.
func (c *CoinGecko) Supports(a model.Asset) bool {
	if a.Kind != model.AssetKindCrypto {
		return false
	}
	_, ok := coingeckoIDBySymbol[strings.ToUpper(a.Symbol)]
	return ok
}

func (c *CoinGecko) Fetch(ctx context.Context, assets []model.Asset, quote model.Asset) ([]Quote, error) {
	if len(assets) == 0 {
		return nil, nil
	}
	// CoinGecko quotes against fiat tickers (usd, eur, …). A non-fiat
	// quote asset can't be served — omit everything (reported as skipped).
	if quote.Kind != model.AssetKindFiat {
		return nil, nil
	}
	vs := strings.ToLower(quote.Symbol)

	ids := make([]string, 0, len(assets))
	assetByID := make(map[string]model.Asset, len(assets))
	for _, a := range assets {
		id, ok := coingeckoIDBySymbol[strings.ToUpper(a.Symbol)]
		if !ok {
			continue // Supports-filtered callers shouldn't hit this; stay safe
		}
		ids = append(ids, id)
		assetByID[id] = a
	}
	if len(ids) == 0 {
		return nil, nil
	}

	u := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=%s",
		c.baseURL, url.QueryEscape(strings.Join(ids, ",")), url.QueryEscape(vs))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("coingecko: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coingecko: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coingecko: unexpected status %d", resp.StatusCode)
	}

	// Decode with UseNumber so prices reach decimal without a float64 hop.
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	var body map[string]map[string]json.Number
	if err := dec.Decode(&body); err != nil {
		return nil, fmt.Errorf("coingecko: decode response: %w", err)
	}

	asOf := c.now().UTC()
	out := make([]Quote, 0, len(body))
	for id, byCurrency := range body {
		a, ok := assetByID[id]
		if !ok {
			continue
		}
		raw, ok := byCurrency[vs]
		if !ok {
			continue // upstream couldn't quote in this currency → skipped
		}
		p, err := decimal.NewFromString(raw.String())
		if err != nil {
			return nil, fmt.Errorf("coingecko: bad price %q for %s: %w", raw.String(), a.Symbol, err)
		}
		out = append(out, Quote{AssetID: a.ID, QuoteAssetID: quote.ID, Price: p, AsOf: asOf})
	}
	return out, nil
}
