package prices

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
)

func TestAlphaVantage_Supports(t *testing.T) {
	a := NewAlphaVantage("test-key")
	cases := []struct {
		name  string
		asset model.Asset
		want  bool
	}{
		{"equity", equityAsset(1, "AAPL"), true},
		{"fund", fundAsset(1, "VTSAX"), true},
		{"crypto", cryptoAsset(1, "BTC"), false},
		{"fiat", fiatAsset(1, "USD"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.Supports(tc.asset); got != tc.want {
				t.Errorf("Supports(%s/%s) = %v, want %v", tc.asset.Symbol, tc.asset.Kind, got, tc.want)
			}
		})
	}
}

// TestAlphaVantage_Fetch_OneCallPerSymbolWithKey: each held symbol gets its
// own GLOBAL_QUOTE call carrying the configured key, paced by WithPause.
func TestAlphaVantage_Fetch_OneCallPerSymbolWithKey(t *testing.T) {
	var symbolsSeen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("function") != "GLOBAL_QUOTE" {
			t.Errorf("function = %q, want GLOBAL_QUOTE", q.Get("function"))
		}
		if q.Get("apikey") != "test-key" {
			t.Errorf("apikey = %q, want test-key", q.Get("apikey"))
		}
		symbol := q.Get("symbol")
		symbolsSeen = append(symbolsSeen, symbol)
		w.Header().Set("Content-Type", "application/json")
		switch symbol {
		case "AAPL":
			fmt.Fprint(w, `{"Global Quote":{"01. symbol":"AAPL","05. price":"185.1400"}}`)
		case "MSFT":
			fmt.Fprint(w, `{}`) // unknown/empty → skipped, not an error
		}
	}))
	defer srv.Close()

	a := NewAlphaVantage("test-key").WithBaseURL(srv.URL).WithPause(0)
	fixed := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return fixed }

	aapl := equityAsset(1, "AAPL")
	msft := equityAsset(2, "MSFT")
	usd := fiatAsset(9, "USD")
	quotes, err := a.Fetch(context.Background(), []model.Asset{aapl, msft}, usd)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(symbolsSeen) != 2 {
		t.Errorf("symbols requested = %v, want 2 calls", symbolsSeen)
	}
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1 (MSFT empty quote); got %+v", len(quotes), quotes)
	}
	q := quotes[0]
	if q.AssetID != aapl.ID || q.QuoteAssetID != usd.ID {
		t.Errorf("quote ids = (%d→%d), want (%d→%d)", q.AssetID, q.QuoteAssetID, aapl.ID, usd.ID)
	}
	if q.Price.String() != "185.14" {
		t.Errorf("price = %s, want 185.14", q.Price)
	}
	if !q.AsOf.Equal(fixed) {
		t.Errorf("asOf = %v, want %v", q.AsOf, fixed)
	}
}

// TestAlphaVantage_Fetch_RateLimitNoteDegradesToSkipped: a 200 response
// carrying a rate-limit "Note" (the free-tier throttle response) must not
// error the batch — it's a coverage gap, not a transport failure.
func TestAlphaVantage_Fetch_RateLimitNoteDegradesToSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Note":"Thank you for using Alpha Vantage! Our standard API call frequency is 5 calls per minute."}`)
	}))
	defer srv.Close()

	a := NewAlphaVantage("test-key").WithBaseURL(srv.URL).WithPause(0)
	quotes, err := a.Fetch(context.Background(), []model.Asset{equityAsset(1, "AAPL")}, fiatAsset(2, "USD"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(quotes) != 0 {
		t.Errorf("got %d quotes, want 0 (rate-limited → skipped)", len(quotes))
	}
}

func TestAlphaVantage_Fetch_NonUSDQuoteReturnsNothing(t *testing.T) {
	a := NewAlphaVantage("test-key").WithBaseURL("http://invalid.invalid") // must not be called
	quotes, err := a.Fetch(context.Background(), []model.Asset{equityAsset(1, "AAPL")}, fiatAsset(2, "EUR"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(quotes) != 0 {
		t.Errorf("got %d quotes, want 0 (provider only covers USD-quoted US listings)", len(quotes))
	}
}

func TestAlphaVantage_Fetch_ServerErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := NewAlphaVantage("test-key").WithBaseURL(srv.URL).WithPause(0)
	_, err := a.Fetch(context.Background(), []model.Asset{equityAsset(1, "AAPL")}, fiatAsset(2, "USD"))
	if err == nil {
		t.Fatal("Fetch: expected error on 500, got nil")
	}
}
