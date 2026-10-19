package plaid_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// TestCreateLinkToken_RedirectURI asserts the wire-level /link/token/create
// body carries redirect_uri when configured (production OAuth institutions,
// #502) — for both new links and update-mode re-auth — and omits it when
// unset so sandbox doesn't need a dashboard-registered URI.
func TestCreateLinkToken_RedirectURI(t *testing.T) {
	const uri = "https://offbook.tail1234.ts.net/plaid/oauth-return"
	cases := []struct {
		name        string
		redirectURI string
		update      bool
		want        any // nil = field absent
	}{
		{"new link with redirect", uri, false, uri},
		{"update mode with redirect", uri, true, uri},
		{"new link without redirect", "", false, nil},
		{"update mode without redirect", "", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]any
			mux := http.NewServeMux()
			mux.HandleFunc("/link/token/create", func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read body: %v", err)
				}
				if err := json.Unmarshal(body, &captured); err != nil {
					t.Errorf("decode body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"link_token": "link-sandbox-x", "expiration": "2026-10-20T00:00:00Z", "request_id": "req-1",
				})
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			client, err := plaidsvc.NewSDKClient(plaidsvc.Config{
				ClientID: "cid", Secret: "csec", Env: srv.URL, RedirectURI: tc.redirectURI,
			})
			if err != nil {
				t.Fatalf("client: %v", err)
			}
			if tc.update {
				_, err = client.CreateUpdateLinkToken(context.Background(), 7, "access-sandbox-x")
			} else {
				_, err = client.CreateLinkToken(context.Background(), 7)
			}
			if err != nil {
				t.Fatalf("create link token: %v", err)
			}
			got, present := captured["redirect_uri"]
			if tc.want == nil {
				if present {
					t.Errorf("redirect_uri = %v, want absent", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("redirect_uri = %v, want %v", got, tc.want)
			}
		})
	}
}
