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

// frankfurterBaseURL is the public, keyless API root. Frankfurter serves
// the ECB daily reference rates as JSON. Overridable for tests.
const frankfurterBaseURL = "https://api.frankfurter.app"

// Frankfurter quotes fiat currencies into other fiat currencies using the
// ECB daily reference rates (ADR-0014 Phase 2). One request per held
// currency: GET /latest?from=EUR&to=USD returns the direct rate, so no
// inverse division (and its precision loss) is needed. No API key — the
// only egress is the currency-pair list.
type Frankfurter struct {
	baseURL string
	client  *http.Client
}

// NewFrankfurter returns a provider hitting the public Frankfurter API.
func NewFrankfurter() *Frankfurter {
	return &Frankfurter{
		baseURL: frankfurterBaseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// WithBaseURL points the provider at a different API root (tests).
func (f *Frankfurter) WithBaseURL(u string) *Frankfurter {
	f.baseURL = u
	return f
}

func (f *Frankfurter) Name() string { return "frankfurter" }

// Supports: any fiat asset. Currencies outside the ECB reference set are
// omitted at Fetch time (the upstream 404s) and surface as skipped.
func (f *Frankfurter) Supports(a model.Asset) bool {
	return a.Kind == model.AssetKindFiat
}

func (f *Frankfurter) Fetch(ctx context.Context, assets []model.Asset, quote model.Asset) ([]Quote, error) {
	if len(assets) == 0 {
		return nil, nil
	}
	if quote.Kind != model.AssetKindFiat {
		return nil, nil // fiat→non-fiat is not an FX rate
	}
	to := strings.ToUpper(quote.Symbol)

	out := make([]Quote, 0, len(assets))
	for _, a := range assets {
		from := strings.ToUpper(a.Symbol)
		if from == to {
			continue // same currency needs no rate
		}
		q, ok, err := f.fetchOne(ctx, from, to)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // currency outside the ECB set → skipped
		}
		q.AssetID = a.ID
		q.QuoteAssetID = quote.ID
		out = append(out, q)
	}
	return out, nil
}

// fetchOne returns the latest reference rate for one currency pair.
// ok=false means the upstream doesn't know the pair (→ skipped); err is
// reserved for transport/protocol failures.
func (f *Frankfurter) fetchOne(ctx context.Context, from, to string) (Quote, bool, error) {
	u := fmt.Sprintf("%s/latest?from=%s&to=%s", f.baseURL, url.QueryEscape(from), url.QueryEscape(to))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Quote{}, false, fmt.Errorf("frankfurter: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return Quote{}, false, fmt.Errorf("frankfurter: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Unknown currency → 404/422 from the API: a coverage gap, not a failure.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity {
		return Quote{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Quote{}, false, fmt.Errorf("frankfurter: unexpected status %d for %s→%s", resp.StatusCode, from, to)
	}

	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	var body struct {
		Date  string                 `json:"date"`
		Rates map[string]json.Number `json:"rates"`
	}
	if err := dec.Decode(&body); err != nil {
		return Quote{}, false, fmt.Errorf("frankfurter: decode response: %w", err)
	}
	raw, ok := body.Rates[to]
	if !ok {
		return Quote{}, false, nil
	}
	price, err := decimal.NewFromString(raw.String())
	if err != nil {
		return Quote{}, false, fmt.Errorf("frankfurter: bad rate %q for %s→%s: %w", raw.String(), from, to, err)
	}
	// The ECB publishes one reference rate per business day; use that date
	// as the observation instant so re-refreshing the same day upserts in
	// place rather than stacking near-duplicate rows.
	asOf, err := time.Parse("2006-01-02", body.Date)
	if err != nil {
		return Quote{}, false, fmt.Errorf("frankfurter: bad date %q: %w", body.Date, err)
	}
	return Quote{Price: price, AsOf: asOf.UTC()}, true, nil
}
