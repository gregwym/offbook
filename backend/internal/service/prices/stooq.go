package prices

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
)

// stooqBaseURL is the public, keyless API root. Overridable for tests.
const stooqBaseURL = "https://stooq.com"

// Stooq is the keyless-default equity/ETF/fund provider (#372, ADR-0014
// equity addendum). It quotes US-listed symbols via Stooq's free end-of-day
// CSV endpoint. Ticker→provider-symbol mapping is a static transform, not a
// lookup table: Stooq's US listings are "<ticker>.us" (lowercased). Coverage
// is intentionally US-only — a non-US ticker simply won't resolve and
// surfaces as skipped, same as any other provider gap.
type Stooq struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

// NewStooq returns a provider hitting the public Stooq API.
func NewStooq() *Stooq {
	return &Stooq{
		baseURL: stooqBaseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
		now:     time.Now,
	}
}

// WithBaseURL points the provider at a different API root (tests).
func (s *Stooq) WithBaseURL(u string) *Stooq {
	s.baseURL = u
	return s
}

func (s *Stooq) Name() string { return "stooq" }

// Supports: equities and funds (ETFs and mutual funds trade on Stooq's feed
// the same way) — crypto and fiat have their own dedicated providers.
func (s *Stooq) Supports(a model.Asset) bool {
	return a.Kind == model.AssetKindEquity || a.Kind == model.AssetKindFund
}

func (s *Stooq) Fetch(ctx context.Context, assets []model.Asset, quote model.Asset) ([]Quote, error) {
	if len(assets) == 0 {
		return nil, nil
	}
	// Stooq's US listings price in USD only; a non-USD quote can't be served.
	if quote.Kind != model.AssetKindFiat || !strings.EqualFold(quote.Symbol, "USD") {
		return nil, nil
	}

	symbols := make([]string, 0, len(assets))
	assetBySymbol := make(map[string]model.Asset, len(assets))
	for _, a := range assets {
		sym := strings.ToLower(a.Symbol) + ".us"
		symbols = append(symbols, sym)
		assetBySymbol[sym] = a
	}

	// f=sd2t2ohlcv: Symbol,Date,Time,Open,High,Low,Close,Volume. One request
	// covers every symbol (comma-separated), so a multi-holding refresh costs
	// Stooq exactly one call regardless of how many equities are held.
	u := fmt.Sprintf("%s/q/l/?s=%s&f=sd2t2ohlcv&h&e=csv",
		s.baseURL, url.QueryEscape(strings.Join(symbols, ",")))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("stooq: build request: %w", err)
	}
	req.Header.Set("Accept", "text/csv")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stooq: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stooq: unexpected status %d", resp.StatusCode)
	}

	rows, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("stooq: decode csv: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil
	}
	symbolCol, closeCol := -1, -1
	for i, h := range rows[0] {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "symbol":
			symbolCol = i
		case "close":
			closeCol = i
		}
	}
	if symbolCol == -1 || closeCol == -1 {
		return nil, fmt.Errorf("stooq: unexpected csv header %v", rows[0])
	}

	asOf := s.now().UTC()
	out := make([]Quote, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if symbolCol >= len(row) || closeCol >= len(row) {
			continue
		}
		a, ok := assetBySymbol[strings.ToLower(row[symbolCol])]
		if !ok {
			continue
		}
		raw := strings.TrimSpace(row[closeCol])
		if raw == "" || strings.EqualFold(raw, "N/D") {
			continue // upstream has no quote for this symbol → skipped
		}
		p, err := decimal.NewFromString(raw)
		if err != nil {
			return nil, fmt.Errorf("stooq: bad price %q for %s: %w", raw, a.Symbol, err)
		}
		out = append(out, Quote{AssetID: a.ID, QuoteAssetID: quote.ID, Price: p, AsOf: asOf})
	}
	return out, nil
}
