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

func TestFrankfurter_Supports(t *testing.T) {
	f := NewFrankfurter()
	cases := []struct {
		name  string
		asset model.Asset
		want  bool
	}{
		{"fiat", fiatAsset(1, "EUR"), true},
		{"exotic fiat (coverage decided at fetch)", fiatAsset(1, "XXX"), true},
		{"crypto", cryptoAsset(1, "BTC"), false},
		{"equity", model.Asset{ID: 1, Symbol: "AAPL", Kind: model.AssetKindEquity}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.Supports(tc.asset); got != tc.want {
				t.Errorf("Supports(%s/%s) = %v, want %v", tc.asset.Symbol, tc.asset.Kind, got, tc.want)
			}
		})
	}
}

// TestFrankfurter_Fetch_DirectRatePerPair: each held currency gets one
// /latest?from=X&to=Y call; the returned rate lands as a decimal string
// with the ECB publication date as the observation instant. Unknown
// currencies (upstream 404) are omitted, not errors. Same-currency pairs
// are skipped without a call.
func TestFrankfurter_Fetch_DirectRatePerPair(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		from := r.URL.Query().Get("from")
		switch from {
		case "EUR":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"date":"2026-06-09","rates":{"USD":1.078642}}`)
		case "XXX":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected from=%s", from)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	f := NewFrankfurter().WithBaseURL(srv.URL)
	eur := fiatAsset(21, "EUR")
	xxx := fiatAsset(22, "XXX")
	usdDupe := fiatAsset(23, "USD") // same as quote → no call, no quote
	usd := fiatAsset(1, "USD")

	quotes, err := f.Fetch(context.Background(), []model.Asset{eur, xxx, usdDupe}, usd)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if calls != 2 {
		t.Errorf("upstream calls = %d, want 2 (EUR + XXX; USD→USD skipped)", calls)
	}
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1 (XXX unknown upstream); got %+v", len(quotes), quotes)
	}
	q := quotes[0]
	if q.AssetID != eur.ID || q.QuoteAssetID != usd.ID {
		t.Errorf("quote ids = (%d→%d), want (%d→%d)", q.AssetID, q.QuoteAssetID, eur.ID, usd.ID)
	}
	if q.Price.String() != "1.078642" {
		t.Errorf("price = %s, want 1.078642", q.Price)
	}
	wantAsOf := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	if !q.AsOf.Equal(wantAsOf) {
		t.Errorf("asOf = %v, want %v (ECB publication date)", q.AsOf, wantAsOf)
	}
}

func TestFrankfurter_Fetch_NonFiatQuoteReturnsNothing(t *testing.T) {
	f := NewFrankfurter().WithBaseURL("http://invalid.invalid") // must not be called
	quotes, err := f.Fetch(context.Background(), []model.Asset{fiatAsset(1, "EUR")}, cryptoAsset(2, "BTC"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(quotes) != 0 {
		t.Errorf("got %d quotes, want 0 (fiat→crypto is not an FX rate)", len(quotes))
	}
}

func TestFrankfurter_Fetch_ServerErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewFrankfurter().WithBaseURL(srv.URL)
	_, err := f.Fetch(context.Background(), []model.Asset{fiatAsset(1, "EUR")}, fiatAsset(2, "USD"))
	if err == nil {
		t.Fatal("Fetch: expected error on 500, got nil")
	}
}
