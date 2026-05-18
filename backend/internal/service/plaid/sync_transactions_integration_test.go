package plaid_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// fakeTxnsSyncServer returns a paginated /transactions/sync response.
// Page 1: 2 added, has_more=true, next_cursor=page2
// Page 2: 1 added, has_more=false, next_cursor=final
// The same plaid_account_id maps to whatever the test wires up beforehand.
func fakeTxnsSyncServer(t *testing.T, plaidAcctID string) (*httptest.Server, *int32) {
	t.Helper()
	var callCount int32

	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := atomic.AddInt32(&callCount, 1)
		switch n {
		case 1:
			// First page: full historical pull, 2 txns
			_ = json.NewEncoder(w).Encode(map[string]any{
				"added": []map[string]any{
					{
						"transaction_id":    "ptx-001",
						"account_id":        plaidAcctID,
						"amount":            5.43, // Plaid: outflow positive
						"iso_currency_code": "USD",
						"name":              "Blue Bottle SF",
						"merchant_name":     "Blue Bottle Coffee",
						"date":              "2026-05-16",
						"authorized_date":   "2026-05-15",
						"pending":           false,
					},
					{
						"transaction_id":    "ptx-002",
						"account_id":        plaidAcctID,
						"amount":            -2000.00, // refund / payroll
						"iso_currency_code": "USD",
						"name":              "Direct Deposit",
						"date":              "2026-05-01",
						"pending":           false,
					},
				},
				"modified":    []any{},
				"removed":     []any{},
				"next_cursor": "page-2-cursor",
				"has_more":    true,
				"request_id":  "req-tx-1",
			})
		case 2:
			// Second page: one more txn, end of stream
			_ = json.NewEncoder(w).Encode(map[string]any{
				"added": []map[string]any{
					{
						"transaction_id":    "ptx-003",
						"account_id":        plaidAcctID,
						"amount":            12.50,
						"iso_currency_code": "USD",
						"name":              "ATM Fee",
						"date":              "2026-04-30",
						"pending":           false,
					},
				},
				"modified":    []any{},
				"removed":     []any{},
				"next_cursor": "final-cursor",
				"has_more":    false,
				"request_id":  "req-tx-2",
			})
		default:
			// Re-sync runs after the first complete drain. Plaid returns
			// nothing new when cursor is current.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"added":       []any{},
				"modified":    []any{},
				"removed":     []any{},
				"next_cursor": "final-cursor",
				"has_more":    false,
				"request_id":  "req-tx-empty",
			})
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Plaid call: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &callCount
}

func TestService_SyncTransactions_PaginatesAndPersistsCursor(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)

	// Pre-seed account + plaid_items so SyncTransactions can resolve.
	const plaidAcctID = "pacct-sync-1"
	acct := &model.Account{
		UserID:          userID,
		Name:            "Test Checking",
		InstitutionSlug: "ins_test",
		AccountType:     "checking",
		Currency:        "USD",
		PlaidAccountID:  &[]string{plaidAcctID}[0],
		IsActive:        true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
	})

	srv, callCount := fakeTxnsSyncServer(t, plaidAcctID)

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{
		UserID:         userID,
		PlaidItemID:    "item-sync-1",
		AccessTokenEnc: enc,
		Status:         "active",
	}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, piiSvc, g)

	// First full pull
	r, err := svc.SyncTransactions(context.Background(), userID, "item-sync-1")
	if err != nil {
		t.Fatalf("SyncTransactions: %v", err)
	}
	if r.Inserted != 3 {
		t.Errorf("inserted=%d, want 3 (across 2 pages)", r.Inserted)
	}
	if atomic.LoadInt32(callCount) != 2 {
		t.Errorf("server hit %d times, want 2 (full pagination drain)", *callCount)
	}

	// Cursor persisted
	persisted, err := itemRepo.GetByPlaidItemID(context.Background(), userID, "item-sync-1")
	if err != nil {
		t.Fatalf("re-fetch item: %v", err)
	}
	if persisted.Cursor == nil || *persisted.Cursor != "final-cursor" {
		t.Errorf("cursor = %v, want final-cursor", persisted.Cursor)
	}
	if persisted.LastSyncedAt == nil {
		t.Error("last_synced_at not persisted")
	}

	// Sign convention: ptx-001 was 5.43 (outflow), stored as -5.43.
	// ptx-002 was -2000 (inflow), stored as +2000.
	var charge model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-001").First(&charge).Error; err != nil {
		t.Fatalf("fetch ptx-001: %v", err)
	}
	if !charge.Amount.Equal(decimal.NewFromFloat(-5.43)) {
		t.Errorf("ptx-001 amount = %s, want -5.43 (sign flipped)", charge.Amount)
	}
	if charge.UserID != userID || charge.AccountID != acct.ID {
		t.Errorf("ptx-001 user/account = %d/%d, want %d/%d", charge.UserID, charge.AccountID, userID, acct.ID)
	}
	if charge.Source != "plaid" {
		t.Errorf("ptx-001 source = %q", charge.Source)
	}

	var deposit model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-002").First(&deposit).Error; err != nil {
		t.Fatalf("fetch ptx-002: %v", err)
	}
	if !deposit.Amount.Equal(decimal.NewFromInt(2000)) {
		t.Errorf("ptx-002 amount = %s, want 2000 (sign flipped)", deposit.Amount)
	}

	// Re-sync: idempotent. Server returns empty on call 3.
	r2, err := svc.SyncTransactions(context.Background(), userID, "item-sync-1")
	if err != nil {
		t.Fatalf("SyncTransactions #2: %v", err)
	}
	if r2.Inserted != 0 {
		t.Errorf("re-sync inserted=%d, want 0", r2.Inserted)
	}
	var rowCount int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND source = 'plaid'", userID).
		Count(&rowCount).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rowCount != 3 {
		t.Errorf("transactions count = %d, want 3 (no duplicates)", rowCount)
	}
}

func TestService_SyncTransactions_UnknownAccountErrors(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	srv, _ := fakeTxnsSyncServer(t, "pacct-never-discovered")

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{
		UserID:         userID,
		PlaidItemID:    "item-orphan",
		AccessTokenEnc: enc,
		Status:         "active",
	}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, piiSvc, g)
	_, err := svc.SyncTransactions(context.Background(), userID, "item-orphan")
	if err == nil {
		t.Fatal("expected error when plaid_account_id can't resolve to a local account")
	}
}
