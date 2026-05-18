package plaid_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), piiSvc, nil, g)

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

// #80: When Plaid sends transactions for an account_id we haven't synced
// yet, the rows must DLQ rather than abort the whole sync — otherwise a
// single stale Plaid mapping permanently blocks the cursor.
func TestService_SyncTransactions_UnknownAccountGoesToDLQ(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	srv, _ := fakeTxnsSyncServer(t, "pacct-never-discovered")

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	syncErrRepo := repository.NewPlaidSyncErrorRepository(g)
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
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.PlaidSyncError{})
	})

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, syncErrRepo, piiSvc, nil, g)
	r, err := svc.SyncTransactions(context.Background(), userID, "item-orphan")
	if err != nil {
		t.Fatalf("SyncTransactions should not return error on per-row mapping failures: %v", err)
	}
	// All 3 fake rows reference an unknown account → all 3 DLQ.
	if r.Failed != 3 {
		t.Errorf("Failed=%d, want 3 (all rows go to DLQ on unknown account)", r.Failed)
	}
	if r.Inserted != 0 {
		t.Errorf("Inserted=%d, want 0", r.Inserted)
	}

	// Cursor must still advance — that's the whole point.
	persisted, _ := itemRepo.GetByPlaidItemID(context.Background(), userID, "item-orphan")
	if persisted.Cursor == nil || *persisted.Cursor != "final-cursor" {
		t.Errorf("cursor = %v, want final-cursor (must advance past poison rows)", persisted.Cursor)
	}
	if persisted.LastSyncStatus != "ok_with_errors" {
		t.Errorf("last_sync_status = %q, want ok_with_errors", persisted.LastSyncStatus)
	}

	// DLQ rows are linked to the item and capture the raw payload + a
	// mapping-class error code.
	dlq, err := syncErrRepo.ListByItem(context.Background(), userID, persisted.ID, true)
	if err != nil {
		t.Fatalf("list dlq: %v", err)
	}
	if len(dlq) != 3 {
		t.Fatalf("dlq rows = %d, want 3", len(dlq))
	}
	for _, row := range dlq {
		if row.ErrorCode != model.PlaidSyncErrorCodeMapping {
			t.Errorf("error_code = %q, want %q", row.ErrorCode, model.PlaidSyncErrorCodeMapping)
		}
		if row.PlaidTransactionID == nil || *row.PlaidTransactionID == "" {
			t.Errorf("plaid_transaction_id not preserved: %+v", row.PlaidTransactionID)
		}
		// raw_payload should be valid JSON containing the plaid_account_id.
		if !strings.Contains(string(row.RawPayload), "pacct-never-discovered") {
			t.Errorf("raw_payload missing account_id: %s", string(row.RawPayload))
		}
	}

	count, err := syncErrRepo.CountUnresolvedByItem(context.Background(), userID, persisted.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("unresolved count = %d, want 3", count)
	}
}

// Mixed batch: 2 rows map cleanly, 1 row references an unknown account.
// Asserts good rows commit, bad row DLQs, cursor advances, status =
// ok_with_errors.
func TestService_SyncTransactions_PartialSuccess(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)

	const goodAcctID = "pacct-mixed-good"
	const badAcctID = "pacct-mixed-bad" // intentionally not seeded
	acct := &model.Account{
		UserID:          userID,
		Name:            "Mixed Checking",
		InstitutionSlug: "ins_test",
		AccountType:     "checking",
		Currency:        "USD",
		PlaidAccountID:  &[]string{goodAcctID}[0],
		IsActive:        true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.PlaidSyncError{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    "mix-good-1",
					"account_id":        goodAcctID,
					"amount":            10.00,
					"iso_currency_code": "USD",
					"name":              "Good 1",
					"date":              "2026-05-10",
				},
				{
					"transaction_id":    "mix-bad-1",
					"account_id":        badAcctID,
					"amount":            20.00,
					"iso_currency_code": "USD",
					"name":              "Bad 1",
					"date":              "2026-05-11",
				},
				{
					"transaction_id":    "mix-good-2",
					"account_id":        goodAcctID,
					"amount":            30.00,
					"iso_currency_code": "USD",
					"name":              "Good 2",
					"date":              "2026-05-12",
				},
			},
			"modified":    []any{},
			"removed":     []any{},
			"next_cursor": "after-mix",
			"has_more":    false,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	syncErrRepo := repository.NewPlaidSyncErrorRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{
		UserID:         userID,
		PlaidItemID:    "item-mix",
		AccessTokenEnc: enc,
		Status:         "active",
	}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, syncErrRepo, piiSvc, nil, g)
	r, err := svc.SyncTransactions(context.Background(), userID, "item-mix")
	if err != nil {
		t.Fatalf("SyncTransactions: %v", err)
	}
	if r.Inserted != 2 || r.Failed != 1 {
		t.Errorf("inserted=%d failed=%d, want 2/1", r.Inserted, r.Failed)
	}

	// Cursor advanced, status reflects partial.
	persisted, _ := itemRepo.GetByPlaidItemID(context.Background(), userID, "item-mix")
	if *persisted.Cursor != "after-mix" {
		t.Errorf("cursor = %v, want after-mix", persisted.Cursor)
	}
	if persisted.LastSyncStatus != "ok_with_errors" {
		t.Errorf("status = %q, want ok_with_errors", persisted.LastSyncStatus)
	}

	// Retry the DLQ row AFTER seeding the missing account. The row should
	// flip to retried_ok and the txn should appear in transactions.
	badAcct := &model.Account{
		UserID:          userID,
		Name:            "Backfilled",
		InstitutionSlug: "ins_test",
		AccountType:     "checking",
		Currency:        "USD",
		PlaidAccountID:  &[]string{badAcctID}[0],
		IsActive:        true,
	}
	if err := g.Create(badAcct).Error; err != nil {
		t.Fatalf("seed bad acct: %v", err)
	}
	// Delete txns referencing this account first so the FK constraint is
	// happy when Cleanup runs (LIFO — this fires before the txn cleanup
	// registered above, hence the explicit txn delete here).
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", badAcct.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, badAcct.ID)
	})

	dlq, _ := syncErrRepo.ListByItem(context.Background(), userID, persisted.ID, true)
	if len(dlq) != 1 {
		t.Fatalf("dlq rows = %d, want 1", len(dlq))
	}
	if err := svc.RetrySyncError(context.Background(), userID, dlq[0].ID); err != nil {
		t.Fatalf("RetrySyncError: %v", err)
	}

	// Row is now resolved.
	got, err := syncErrRepo.Get(context.Background(), userID, dlq[0].ID)
	if err != nil {
		t.Fatalf("re-fetch dlq row: %v", err)
	}
	if got.Resolution == nil || *got.Resolution != model.ResolutionRetriedOK {
		t.Errorf("resolution = %v, want retried_ok", got.Resolution)
	}

	// And the txn landed.
	var retried model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "mix-bad-1").First(&retried).Error; err != nil {
		t.Fatalf("retried txn missing: %v", err)
	}
	if retried.AccountID != badAcct.ID {
		t.Errorf("retried account_id = %d, want %d", retried.AccountID, badAcct.ID)
	}

	// Unresolved count returns to 0.
	count, _ := syncErrRepo.CountUnresolvedByItem(context.Background(), userID, persisted.ID)
	if count != 0 {
		t.Errorf("unresolved count = %d after retry, want 0", count)
	}
}

// Multi-tenant safety: user A's DLQ rows must not be visible to user B,
// even if they happen to share a numeric error_id.
func TestService_SyncErrors_TenantIsolation(t *testing.T) {
	g := openPlaidTestDB(t)
	userA := seedPlaidTestUser(t, g)
	userB := seedPlaidTestUser(t, g)

	// Seed an item + DLQ row for user A.
	box, _ := crypto.NewSecretBox(newTestKey())
	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	itemA := &model.PlaidItem{UserID: userA, PlaidItemID: "iso-item-A", AccessTokenEnc: enc, Status: "active"}
	if err := g.Create(itemA).Error; err != nil {
		t.Fatalf("seed item: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id IN ?", []int64{userA, userB}).Delete(&model.PlaidSyncError{})
		g.Unscoped().Delete(&model.PlaidItem{}, itemA.ID)
	})

	rowA := &model.PlaidSyncError{
		UserID:       userA,
		PlaidItemID:  itemA.ID,
		RawPayload:   json.RawMessage(`{}`),
		ErrorCode:    model.PlaidSyncErrorCodeMapping,
		ErrorMessage: "test",
	}
	if err := g.Create(rowA).Error; err != nil {
		t.Fatalf("seed dlq: %v", err)
	}

	syncErrRepo := repository.NewPlaidSyncErrorRepository(g)

	// User B cannot Get user A's row.
	if _, err := syncErrRepo.Get(context.Background(), userB, rowA.ID); err == nil {
		t.Error("user B got user A's DLQ row — tenant leak")
	}
	// User B cannot Resolve user A's row.
	if err := syncErrRepo.MarkResolved(context.Background(), userB, rowA.ID, model.ResolutionDismissed, persistedNow()); err == nil {
		t.Error("user B resolved user A's DLQ row — tenant leak")
	}
	// And user A's row stays unresolved.
	gotA, _ := syncErrRepo.Get(context.Background(), userA, rowA.ID)
	if gotA.ResolvedAt != nil {
		t.Error("user A's row was unexpectedly resolved")
	}
}

func persistedNow() time.Time { return time.Now().UTC() }
