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

func equityAsset(id int64, symbol string) model.Asset {
	return model.Asset{ID: id, Symbol: symbol, Kind: model.AssetKindEquity}
}

func fundAsset(id int64, symbol string) model.Asset {
	return model.Asset{ID: id, Symbol: symbol, Kind: model.AssetKindFund}
}

func TestStooq_Supports(t *testing.T) {
	s := NewStooq()
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
			if got := s.Supports(tc.asset); got != tc.want {
				t.Errorf("Supports(%s/%s) = %v, want %v", tc.asset.Symbol, tc.asset.Kind, got, tc.want)
			}
		})
	}
}

// TestStooq_Fetch_SingleRequestCoversAllSymbols: holding N equities costs
// Stooq exactly one HTTP call (comma-joined symbols), and an "N/D" close
// (upstream has no quote) is omitted rather than erroring.
func TestStooq_Fetch_SingleRequestCoversAllSymbols(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.URL.Query().Get("s"); got != "aapl.us,ghost.us" {
			t.Errorf("s = %q, want aapl.us,ghost.us", got)
		}
		fmt.Fprint(w, "Symbol,Date,Time,Open,High,Low,Close,Volume\n")
		fmt.Fprint(w, "AAPL.US,2026-06-10,22:00:00,185.0,186.0,184.0,185.14,1000\n")
		fmt.Fprint(w, "GHOST.US,2026-06-10,22:00:00,N/D,N/D,N/D,N/D,0\n")
	}))
	defer srv.Close()

	s := NewStooq().WithBaseURL(srv.URL)
	fixed := time.Date(2026, 6, 10, 22, 5, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }

	aapl := equityAsset(1, "AAPL")
	ghost := equityAsset(2, "GHOST")
	usd := fiatAsset(9, "USD")
	quotes, err := s.Fetch(context.Background(), []model.Asset{aapl, ghost}, usd)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if calls != 1 {
		t.Errorf("upstream calls = %d, want 1 (batched)", calls)
	}
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1 (GHOST is N/D); got %+v", len(quotes), quotes)
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

func TestStooq_Fetch_NonUSDQuoteReturnsNothing(t *testing.T) {
	s := NewStooq().WithBaseURL("http://invalid.invalid") // must not be called
	quotes, err := s.Fetch(context.Background(), []model.Asset{equityAsset(1, "AAPL")}, fiatAsset(2, "EUR"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(quotes) != 0 {
		t.Errorf("got %d quotes, want 0 (Stooq US feed only quotes in USD)", len(quotes))
	}
}

func TestStooq_Fetch_ServerErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewStooq().WithBaseURL(srv.URL)
	_, err := s.Fetch(context.Background(), []model.Asset{equityAsset(1, "AAPL")}, fiatAsset(2, "USD"))
	if err == nil {
		t.Fatal("Fetch: expected error on 500, got nil")
	}
}
