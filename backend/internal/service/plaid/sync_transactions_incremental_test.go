package plaid_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// fakeIncrementalServer responds to /transactions/sync with a single page
// containing 1 added / 1 modified / 1 removed. Asserts that the request
// body carries the persisted cursor (so we know incremental mode kicked in).
func fakeIncrementalServer(t *testing.T, plaidAcctID, modifiedTxnID, removedTxnID, expectCursor string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if got, _ := body["cursor"].(string); got != expectCursor {
			t.Errorf("incremental sync cursor = %q, want %q", got, expectCursor)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    "ptx-new-1",
					"account_id":        plaidAcctID,
					"amount":            7.77,
					"iso_currency_code": "USD",
					"name":              "Fresh Charge",
					"date":              "2026-05-17",
					"pending":           false,
				},
			},
			"modified": []map[string]any{
				{
					"transaction_id":    modifiedTxnID,
					"account_id":        plaidAcctID,
					"amount":            55.55, // changed from prior amount
					"iso_currency_code": "USD",
					"name":              "Updated description",
					"date":              "2026-05-12",
					"pending":           false,
				},
			},
			"removed": []map[string]any{
				{"transaction_id": removedTxnID},
			},
			"next_cursor": "incremental-next-cursor",
			"has_more":    false,
			"request_id":  "req-incr",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Plaid call: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestService_SyncTransactions_IncrementalAddedModifiedRemoved(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	const plaidAcctID = "pacct-incr-1"
	const priorCursor = "cursor-from-prior-sync"

	// Pre-seed an account and three existing Plaid-sourced transactions.
	// One will be modified (with a user-set category_id + notes that must
	// survive), one will be removed (soft-deleted), one is untouched.
	acct := &model.Account{
		UserID:          userID,
		Name:            "Test Account",
		InstitutionSlug: "ins_test",
		AccountType:     "checking",
		Currency:        "USD",
		PlaidAccountID:  &[]string{plaidAcctID}[0],
		IsActive:        true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}

	// Seed a category for the "user already categorized" case.
	cat := &model.Category{Name: "TestCat-Incr", Slug: "test-cat-incr"}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}

	priorAmount := decimal.NewFromFloat(-50.00)
	categoryID := cat.ID
	priorNotes := "User note that must survive"
	modified := &model.Transaction{
		UserID:             userID,
		AccountID:          acct.ID,
		Amount:             priorAmount,
		Currency:           "USD",
		Description:        ptr("Pending charge"),
		TransactionDate:    mustDate("2026-05-10"),
		Source:             "plaid",
		PlaidTransactionID: ptr("ptx-to-modify"),
		ExternalID:         ptr("ptx-to-modify"),
		CategoryID:         &categoryID,
		Notes:              &priorNotes,
	}
	removed := &model.Transaction{
		UserID:             userID,
		AccountID:          acct.ID,
		Amount:             decimal.NewFromFloat(-10.00),
		Currency:           "USD",
		Description:        ptr("Will be removed"),
		TransactionDate:    mustDate("2026-05-09"),
		Source:             "plaid",
		PlaidTransactionID: ptr("ptx-to-remove"),
		ExternalID:         ptr("ptx-to-remove"),
	}
	untouched := &model.Transaction{
		UserID:             userID,
		AccountID:          acct.ID,
		Amount:             decimal.NewFromFloat(-1.00),
		Currency:           "USD",
		Description:        ptr("Survives"),
		TransactionDate:    mustDate("2026-05-08"),
		Source:             "plaid",
		PlaidTransactionID: ptr("ptx-untouched"),
		ExternalID:         ptr("ptx-untouched"),
	}
	for _, row := range []*model.Transaction{modified, removed, untouched} {
		if err := g.Create(row).Error; err != nil {
			t.Fatalf("seed tx: %v", err)
		}
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
		g.Unscoped().Delete(&model.Category{}, cat.ID)
	})

	// Pre-seed plaid_items with a stored cursor so the request body carries it.
	box, _ := crypto.NewSecretBox(newTestKey())
	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	cursorVal := priorCursor
	item := &model.PlaidItem{
		UserID:         userID,
		PlaidItemID:    "item-incr-1",
		AccessTokenEnc: enc,
		Status:         "active",
		Cursor:         &cursorVal,
	}
	if err := g.Create(item).Error; err != nil {
		t.Fatalf("seed item: %v", err)
	}

	srv := fakeIncrementalServer(t, plaidAcctID, "ptx-to-modify", "ptx-to-remove", priorCursor)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))
	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), piiSvc, nil, g)

	r, err := svc.SyncTransactions(context.Background(), userID, "item-incr-1")
	if err != nil {
		t.Fatalf("SyncTransactions: %v", err)
	}
	if r.Inserted != 1 || r.Modified != 1 || r.Removed != 1 {
		t.Errorf("inserted=%d modified=%d removed=%d, want 1/1/1", r.Inserted, r.Modified, r.Removed)
	}

	// Added: brand-new row exists.
	var added model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-new-1").First(&added).Error; err != nil {
		t.Fatalf("fetch added: %v", err)
	}
	if !added.Amount.Equal(decimal.NewFromFloat(-7.77)) {
		t.Errorf("added amount = %s, want -7.77", added.Amount)
	}

	// Modified: amount updated; user-set category_id + notes preserved.
	var afterMod model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-to-modify").First(&afterMod).Error; err != nil {
		t.Fatalf("fetch modified: %v", err)
	}
	if !afterMod.Amount.Equal(decimal.NewFromFloat(-55.55)) {
		t.Errorf("modified amount = %s, want -55.55 (Plaid-owned, should update)", afterMod.Amount)
	}
	if afterMod.CategoryID == nil || *afterMod.CategoryID != cat.ID {
		t.Errorf("modified category_id = %v, want %d (user-edited, must survive)", afterMod.CategoryID, cat.ID)
	}
	if afterMod.Notes == nil || *afterMod.Notes != "User note that must survive" {
		t.Errorf("modified notes = %v, want preserved", afterMod.Notes)
	}
	if afterMod.Description == nil || *afterMod.Description != "Updated description" {
		t.Errorf("modified description = %v, want Plaid update", afterMod.Description)
	}

	// Removed: soft-deleted, not gone from the table.
	var rmCount int64
	if err := g.Unscoped().Model(&model.Transaction{}).
		Where("plaid_transaction_id = ?", "ptx-to-remove").Count(&rmCount).Error; err != nil {
		t.Fatalf("count removed: %v", err)
	}
	if rmCount != 1 {
		t.Errorf("removed row count (unscoped) = %d, want 1 (soft-delete preserves)", rmCount)
	}
	// And the scoped query excludes it.
	var scoped int64
	if err := g.Model(&model.Transaction{}).
		Where("plaid_transaction_id = ?", "ptx-to-remove").Count(&scoped).Error; err != nil {
		t.Fatalf("scoped count: %v", err)
	}
	if scoped != 0 {
		t.Errorf("removed row count (scoped) = %d, want 0", scoped)
	}

	// Untouched: still there, unchanged.
	var u model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-untouched").First(&u).Error; err != nil {
		t.Fatalf("fetch untouched: %v", err)
	}
	if !u.Amount.Equal(decimal.NewFromFloat(-1)) {
		t.Errorf("untouched amount = %s, want -1", u.Amount)
	}

	// Cursor advanced exactly once to the value returned with has_more=false.
	persisted, err := itemRepo.GetByPlaidItemID(context.Background(), userID, "item-incr-1")
	if err != nil {
		t.Fatalf("fetch item: %v", err)
	}
	if persisted.Cursor == nil || *persisted.Cursor != "incremental-next-cursor" {
		t.Errorf("cursor = %v, want incremental-next-cursor", persisted.Cursor)
	}
}

func TestService_SyncTransactions_TenantIsolation(t *testing.T) {
	g := openPlaidTestDB(t)
	userA := seedPlaidTestUser(t, g)
	userB := seedPlaidTestUser(t, g)

	// Both users have a Plaid account on the SAME upstream institution
	// (different IDs locally; same Plaid account_id namespace would be a
	// realistic two-user-of-same-bank scenario). Pre-seed one transaction
	// for user B that must NOT be touched.
	const plaidAcctIDA = "pacct-A"
	const plaidAcctIDB = "pacct-B"
	acctA := &model.Account{UserID: userA, Name: "A acct", InstitutionSlug: "ins_test", AccountType: "checking", Currency: "USD", PlaidAccountID: ptr(plaidAcctIDA), IsActive: true}
	acctB := &model.Account{UserID: userB, Name: "B acct", InstitutionSlug: "ins_test", AccountType: "checking", Currency: "USD", PlaidAccountID: ptr(plaidAcctIDB), IsActive: true}
	if err := g.Create(acctA).Error; err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := g.Create(acctB).Error; err != nil {
		t.Fatalf("seed B: %v", err)
	}
	bTxn := &model.Transaction{
		UserID: userB, AccountID: acctB.ID,
		Amount: decimal.NewFromFloat(-99), Currency: "USD",
		Description: ptr("B's private tx"), TransactionDate: mustDate("2026-05-01"),
		Source: "plaid", PlaidTransactionID: ptr("ptx-B-only"), ExternalID: ptr("ptx-B-only"),
	}
	if err := g.Create(bTxn).Error; err != nil {
		t.Fatalf("seed B tx: %v", err)
	}
	bItemCursor := "B-cursor-untouched"
	bItem := &model.PlaidItem{UserID: userB, PlaidItemID: "item-B", AccessTokenEnc: []byte("dummy-not-used"), Status: "active", Cursor: &bItemCursor}
	if err := g.Create(bItem).Error; err != nil {
		t.Fatalf("seed B item: %v", err)
	}

	t.Cleanup(func() {
		for _, uid := range []int64{userA, userB} {
			g.Unscoped().Where("user_id = ?", uid).Delete(&model.Transaction{})
			g.Unscoped().Where("user_id = ?", uid).Delete(&model.Account{})
			g.Unscoped().Where("user_id = ?", uid).Delete(&model.PlaidItem{})
		}
	})

	// User A's item gets a fresh sync that ADDS one txn for A's account.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transactions/sync" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    "ptx-A-1",
					"account_id":        plaidAcctIDA,
					"amount":            10.00,
					"iso_currency_code": "USD",
					"name":              "A's coffee",
					"date":              "2026-05-15",
					"pending":           false,
				},
			},
			"modified":    []any{},
			"removed":     []any{},
			"next_cursor": "A-new-cursor",
			"has_more":    false,
			"request_id":  "req-iso",
		})
	}))
	t.Cleanup(srv.Close)

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	enc, _ := box.Encrypt([]byte("access-sandbox-A"))
	aItem := &model.PlaidItem{UserID: userA, PlaidItemID: "item-A", AccessTokenEnc: enc, Status: "active"}
	if err := g.Create(aItem).Error; err != nil {
		t.Fatalf("seed A item: %v", err)
	}

	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))
	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), piiSvc, nil, g)

	if _, err := svc.SyncTransactions(context.Background(), userA, "item-A"); err != nil {
		t.Fatalf("Sync user A: %v", err)
	}

	// User B's transaction must still be there and unchanged.
	var bAfter model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-B-only").First(&bAfter).Error; err != nil {
		t.Fatalf("B tx after A sync: %v", err)
	}
	if bAfter.UserID != userB {
		t.Errorf("B tx user_id corrupted: got %d, want %d", bAfter.UserID, userB)
	}

	// B's plaid_items cursor must NOT have advanced.
	bItemAfter, err := itemRepo.GetByPlaidItemID(context.Background(), userB, "item-B")
	if err != nil {
		t.Fatalf("B item after A sync: %v", err)
	}
	if bItemAfter.Cursor == nil || *bItemAfter.Cursor != "B-cursor-untouched" {
		t.Errorf("B cursor = %v, want B-cursor-untouched (untouched by A's sync)", bItemAfter.Cursor)
	}

	// A's new transaction did land.
	var aTx model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-A-1").First(&aTx).Error; err != nil {
		t.Fatalf("A tx not inserted: %v", err)
	}
	if aTx.UserID != userA {
		t.Errorf("A tx user_id = %d, want %d", aTx.UserID, userA)
	}
}
