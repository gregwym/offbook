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

// TestSyncTransactions_RequestsPersonalFinanceCategory asserts the wire-level
// request body sent to /transactions/sync includes
// `options.include_personal_finance_category: true`. Without this flag Plaid
// omits PFC for new items, leaving every transaction uncategorized — see #181.
func TestSyncTransactions_RequestsPersonalFinanceCategory(t *testing.T) {
	var captured map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("decode body: %v (body=%q)", err, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []any{}, "modified": []any{}, "removed": []any{},
			"next_cursor": "", "has_more": false, "request_id": "req-pfc",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Plaid call: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := plaidsvc.NewSDKClient(plaidsvc.Config{
		ClientID: "cid",
		Secret:   "csec",
		Env:      srv.URL,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, err := client.SyncTransactions(context.Background(), "access-sandbox-x", ""); err != nil {
		t.Fatalf("SyncTransactions: %v", err)
	}

	opts, ok := captured["options"].(map[string]any)
	if !ok {
		t.Fatalf("request body missing `options` block (captured=%v)", captured)
	}
	got, ok := opts["include_personal_finance_category"].(bool)
	if !ok {
		t.Fatalf("options.include_personal_finance_category missing or wrong type (options=%v)", opts)
	}
	if !got {
		t.Errorf("include_personal_finance_category = false, want true")
	}
}
