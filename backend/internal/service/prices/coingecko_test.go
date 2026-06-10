package prices

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
)

func cryptoAsset(id int64, symbol string) model.Asset {
	return model.Asset{ID: id, Symbol: symbol, Kind: model.AssetKindCrypto}
}

func fiatAsset(id int64, symbol string) model.Asset {
	return model.Asset{ID: id, Symbol: symbol, Kind: model.AssetKindFiat}
}

func TestCoinGecko_Supports(t *testing.T) {
	c := NewCoinGecko()
	cases := []struct {
		name  string
		asset model.Asset
		want  bool
	}{
		{"mapped crypto", cryptoAsset(1, "BTC"), true},
		{"mapped crypto lowercase", cryptoAsset(1, "eth"), true},
		{"unmapped crypto", cryptoAsset(1, "OBSCURECOIN"), false},
		{"equity with crypto-like symbol", model.Asset{ID: 1, Symbol: "BTC", Kind: model.AssetKindEquity}, false},
		{"fiat", fiatAsset(1, "USD"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.Supports(tc.asset); got != tc.want {
				t.Errorf("Supports(%s/%s) = %v, want %v", tc.asset.Symbol, tc.asset.Kind, got, tc.want)
			}
		})
	}
}

// TestCoinGecko_Fetch_ParsesWithoutFloat64: prices arrive as decimal strings
// straight from the JSON number token — a high-precision quote must survive
// the trip exactly.
func TestCoinGecko_Fetch_ParsesWithoutFloat64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/simple/price" {
			t.Errorf("path = %s, want /simple/price", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("vs_currencies") != "usd" {
			t.Errorf("vs_currencies = %q, want usd", q.Get("vs_currencies"))
		}
		w.Header().Set("Content-Type", "application/json")
		// "unknowncoin" is unrequested noise; "ethereum" omits the usd quote.
		_, _ = w.Write([]byte(`{
			"bitcoin": {"usd": 67123.456789012345678},
			"ethereum": {"eur": 3000.5},
			"unknowncoin": {"usd": 1}
		}`))
	}))
	defer srv.Close()

	c := NewCoinGecko().WithBaseURL(srv.URL)
	fixed := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return fixed }

	btc := cryptoAsset(11, "BTC")
	eth := cryptoAsset(12, "ETH")
	usd := fiatAsset(1, "USD")
	quotes, err := c.Fetch(context.Background(), []model.Asset{btc, eth}, usd)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1 (ETH had no usd quote, unknowncoin unrequested); got %+v", len(quotes), quotes)
	}
	q := quotes[0]
	if q.AssetID != btc.ID || q.QuoteAssetID != usd.ID {
		t.Errorf("quote ids = (%d→%d), want (%d→%d)", q.AssetID, q.QuoteAssetID, btc.ID, usd.ID)
	}
	if q.Price.String() != "67123.456789012345678" {
		t.Errorf("price = %s, want 67123.456789012345678 (no float64 precision loss)", q.Price)
	}
	if !q.AsOf.Equal(fixed) {
		t.Errorf("asOf = %v, want %v", q.AsOf, fixed)
	}
}

func TestCoinGecko_Fetch_NonFiatQuoteReturnsNothing(t *testing.T) {
	c := NewCoinGecko().WithBaseURL("http://invalid.invalid") // must not be called
	quotes, err := c.Fetch(context.Background(), []model.Asset{cryptoAsset(1, "BTC")}, cryptoAsset(2, "ETH"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(quotes) != 0 {
		t.Errorf("got %d quotes, want 0 (crypto quote currency unsupported)", len(quotes))
	}
}

func TestCoinGecko_Fetch_UpstreamErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewCoinGecko().WithBaseURL(srv.URL)
	_, err := c.Fetch(context.Background(), []model.Asset{cryptoAsset(1, "BTC")}, fiatAsset(2, "USD"))
	if err == nil {
		t.Fatal("Fetch: expected error on 429, got nil")
	}
}
