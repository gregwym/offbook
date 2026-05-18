package plaid_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// flippableSyncServer alternates between success and failure on /transactions/sync:
//
//	call 1 → 500 (forces sync into the error path)
//	call 2 → 200 with one txn, has_more=false (success path)
//
// All other paths 500 with an error in the body so we can verify the
// safeSyncErrorMessage redaction path handles raw error strings (no
// PlaidError extraction available since httptest doesn't echo their
// canonical shape).
func flippableSyncServer(t *testing.T, plaidAcctID string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			// Body shaped like a Plaid error so safeSyncErrorMessage can
			// pluck error_code + display_message via the PlaidError path.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error_type":      "ITEM_ERROR",
				"error_code":      "ITEM_LOGIN_REQUIRED",
				"error_message":   "the login details of this item have changed",
				"display_message": "Please reconnect your account.",
				"request_id":      "req-secret-123",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    "ptx-status-ok-1",
					"account_id":        plaidAcctID,
					"amount":            7.50,
					"iso_currency_code": "USD",
					"name":              "Stable after error",
					"date":              "2026-05-12",
					"pending":           false,
				},
			},
			"modified":    []any{},
			"removed":     []any{},
			"next_cursor": "cursor-recovered",
			"has_more":    false,
			"request_id":  "req-ok",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Plaid call: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

// TestPlaidSync_StatusFlipsOnErrorThenClearsOnSuccess proves the #65
// lifecycle wiring end-to-end:
//   - a sync that errors leaves last_sync_status='error' with a populated
//     last_sync_error and DOES NOT advance cursor / last_synced_at
//   - a subsequent successful sync flips status to 'ok', clears the error,
//     and advances last_synced_at
func TestPlaidSync_StatusFlipsOnErrorThenClearsOnSuccess(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	const plaidAcctID = "pacct-status-1"

	acct := &model.Account{
		UserID: userID, Name: "Status Acct", InstitutionSlug: "ins_test",
		AccountType: "checking", Currency: "USD",
		PlaidAccountID: ptr(plaidAcctID), IsActive: true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
	})

	srv, calls := flippableSyncServer(t, plaidAcctID)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{
		UserID: userID, PlaidItemID: "item-status", AccessTokenEnc: enc,
		Status: "active", LastSyncStatus: "never",
	}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), piiSvc, nil, g)

	// Phase 1: forced error.
	if _, err := svc.SyncTransactions(context.Background(), userID, "item-status"); err == nil {
		t.Fatal("sync #1: expected error from upstream 500")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("calls after sync #1 = %d, want 1", got)
	}

	var afterErr model.PlaidItem
	if err := g.First(&afterErr, item.ID).Error; err != nil {
		t.Fatalf("reload after error: %v", err)
	}
	if afterErr.LastSyncStatus != "error" {
		t.Errorf("last_sync_status = %q, want error", afterErr.LastSyncStatus)
	}
	if afterErr.LastSyncError == nil || *afterErr.LastSyncError == "" {
		t.Fatalf("last_sync_error empty after failed sync")
	}
	// Redaction: request_id must not leak into the user-facing message.
	if got := *afterErr.LastSyncError; containsSubstring(got, "req-secret-123") {
		t.Errorf("last_sync_error leaked request_id: %q", got)
	}
	if afterErr.LastSyncedAt != nil {
		t.Errorf("last_synced_at advanced on failure: %v", afterErr.LastSyncedAt)
	}
	if afterErr.Cursor != nil {
		t.Errorf("cursor advanced on failure: %v", afterErr.Cursor)
	}

	// Phase 2: success on retry.
	r, err := svc.SyncTransactions(context.Background(), userID, "item-status")
	if err != nil {
		t.Fatalf("sync #2: %v", err)
	}
	if r.Inserted != 1 {
		t.Errorf("sync #2: inserted=%d, want 1", r.Inserted)
	}

	var afterOK model.PlaidItem
	if err := g.First(&afterOK, item.ID).Error; err != nil {
		t.Fatalf("reload after success: %v", err)
	}
	if afterOK.LastSyncStatus != "ok" {
		t.Errorf("last_sync_status = %q, want ok", afterOK.LastSyncStatus)
	}
	if afterOK.LastSyncError != nil {
		t.Errorf("last_sync_error not cleared on success: %v", *afterOK.LastSyncError)
	}
	if afterOK.LastSyncedAt == nil {
		t.Errorf("last_synced_at not set on success")
	}
	if afterOK.Cursor == nil || *afterOK.Cursor != "cursor-recovered" {
		t.Errorf("cursor = %v, want cursor-recovered", afterOK.Cursor)
	}
}

func containsSubstring(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
