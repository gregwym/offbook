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

// fakeFixedPayloadServer always returns the same /transactions/sync payload
// for the FIRST drain (page 1 has 2 txns, has_more=false), and an empty
// delta for every subsequent call regardless of cursor. That mimics what
// Plaid does once a cursor catches up — a stable end-state we can run a
// re-sync against.
//
// Note for #63: the cursor advances after the first drain, so a re-sync
// (call #2) goes through the "no new changes" path. But the dedup guarantee
// we actually want to assert is "if Plaid ever replays the same payload
// (cursor reset, account re-link, etc.), we don't double up." So we make
// the server ignore the cursor and replay the payload whenever the caller
// sends an empty cursor — and we drive that by manually clearing the cursor
// in the DB between syncs.
func fakeFixedPayloadServer(t *testing.T, plaidAcctID string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    "ptx-dedup-1",
					"account_id":        plaidAcctID,
					"amount":            3.14,
					"iso_currency_code": "USD",
					"name":              "Stable charge A",
					"date":              "2026-05-10",
					"pending":           false,
				},
				{
					"transaction_id":    "ptx-dedup-2",
					"account_id":        plaidAcctID,
					"amount":            -100.00, // inflow
					"iso_currency_code": "USD",
					"name":              "Stable deposit B",
					"date":              "2026-05-11",
					"pending":           false,
				},
			},
			"modified":    []any{},
			"removed":     []any{},
			"next_cursor": "stable-cursor",
			"has_more":    false,
			"request_id":  "req-dedup",
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

func TestPlaidSync_NoDuplicatesOnReSync(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	const plaidAcctID = "pacct-dedup-1"

	acct := &model.Account{
		UserID: userID, Name: "Dedup Acct", InstitutionSlug: "ins_test",
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

	srv, _ := fakeFixedPayloadServer(t, plaidAcctID)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{UserID: userID, PlaidItemID: "item-dedup", AccessTokenEnc: enc, Status: "active"}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)

	// First sync: 2 inserts.
	r1, err := svc.SyncTransactions(context.Background(), userID, "item-dedup")
	if err != nil {
		t.Fatalf("sync #1: %v", err)
	}
	if r1.Inserted != 2 {
		t.Errorf("sync #1: inserted=%d, want 2", r1.Inserted)
	}

	// Simulate a cursor reset (account re-link, Plaid item recovery, etc.)
	// by clearing the persisted cursor. Without dedup the same payload
	// would now insert 2 more rows.
	if err := g.Model(&model.PlaidItem{}).
		Where("id = ?", item.ID).
		Update("cursor", nil).Error; err != nil {
		t.Fatalf("reset cursor: %v", err)
	}

	r2, err := svc.SyncTransactions(context.Background(), userID, "item-dedup")
	if err != nil {
		t.Fatalf("sync #2: %v", err)
	}
	if r2.Inserted != 0 {
		t.Errorf("sync #2: inserted=%d, want 0 (ON CONFLICT DO NOTHING)", r2.Inserted)
	}
	if r2.Resurrected != 0 {
		t.Errorf("sync #2: resurrected=%d, want 0 (rows are still live)", r2.Resurrected)
	}

	// Count: still exactly 2.
	var n int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND source = 'plaid'", userID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("transactions count = %d, want 2 (no duplicates)", n)
	}

	// Same IDs, in case anyone replaces ON CONFLICT with INSERT.
	for _, ptxID := range []string{"ptx-dedup-1", "ptx-dedup-2"} {
		var c int64
		if err := g.Model(&model.Transaction{}).
			Where("plaid_transaction_id = ?", ptxID).Count(&c).Error; err != nil {
			t.Fatalf("count %s: %v", ptxID, err)
		}
		if c != 1 {
			t.Errorf("%s: %d rows, want 1", ptxID, c)
		}
	}
}

// TestPlaidSync_SoftDeletedReSurfaces verifies the resurrect-in-place
// behavior documented in #63: if the user (or a prior /removed entry)
// soft-deleted a Plaid-imported row and Plaid later replays the same
// transaction_id in `added`, we clear deleted_at on the existing row
// rather than insert a duplicate. The choice is restore-over-insert
// because a Plaid row has no PII and is unambiguously the same financial
// event — silently splitting it into two rows would corrupt history.
//
// User-edited fields (notes, category_id) must survive the resurrect.
func TestPlaidSync_SoftDeletedReSurfaces(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	const plaidAcctID = "pacct-resurface-1"

	acct := &model.Account{
		UserID: userID, Name: "Resurface Acct", InstitutionSlug: "ins_test",
		AccountType: "checking", Currency: "USD",
		PlaidAccountID: ptr(plaidAcctID), IsActive: true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}

	cat := &model.Category{Name: "ResurrectCat", Slug: "resurrect-cat"}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed cat: %v", err)
	}

	// Seed a Plaid-sourced row, then soft-delete it. Carries user-set
	// notes + category that the resurrect path must preserve.
	priorNotes := "Notes attached by user before deletion"
	categoryID := cat.ID
	original := &model.Transaction{
		UserID:             userID,
		AccountID:          acct.ID,
		Amount:             decimal.NewFromFloat(-3.14),
		Description:        ptr("Stable charge A"),
		TransactionDate:    mustDate("2026-05-10"),
		Source:             "plaid",
		PlaidTransactionID: ptr("ptx-dedup-1"),
		ExternalID:         ptr("ptx-dedup-1"),
		CategoryID:         &categoryID,
		Notes:              &priorNotes,
	}
	if err := g.Create(original).Error; err != nil {
		t.Fatalf("seed original: %v", err)
	}
	originalID := original.ID
	// Soft-delete via the repo path so we exercise the same code path the
	// user would.
	txRepo := repository.NewTransactionRepository(g)
	if err := txRepo.SoftDelete(context.Background(), userID, originalID); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}

	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
		g.Unscoped().Delete(&model.Category{}, cat.ID)
	})

	// Re-sync: payload includes the soft-deleted row's plaid_transaction_id.
	srv, _ := fakeFixedPayloadServer(t, plaidAcctID)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{UserID: userID, PlaidItemID: "item-resurface", AccessTokenEnc: enc, Status: "active"}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)

	r, err := svc.SyncTransactions(context.Background(), userID, "item-resurface")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if r.Resurrected != 1 {
		t.Errorf("resurrected=%d, want 1 (ptx-dedup-1 was soft-deleted, payload re-added it)", r.Resurrected)
	}
	if r.Inserted != 1 {
		t.Errorf("inserted=%d, want 1 (only ptx-dedup-2 was new)", r.Inserted)
	}

	// Total rows for this account (unscoped + scoped) == 2: the resurrected
	// row + the new one. NOT 3 (would mean we duplicated rather than
	// restored).
	var unscoped int64
	if err := g.Unscoped().Model(&model.Transaction{}).
		Where("user_id = ?", userID).Count(&unscoped).Error; err != nil {
		t.Fatalf("count unscoped: %v", err)
	}
	if unscoped != 2 {
		t.Errorf("unscoped row count = %d, want 2 (resurrect, not duplicate)", unscoped)
	}

	// The resurrected row is the SAME id as before — proves restore-in-place.
	var restored model.Transaction
	if err := g.Where("plaid_transaction_id = ?", "ptx-dedup-1").First(&restored).Error; err != nil {
		t.Fatalf("fetch restored: %v", err)
	}
	if restored.ID != originalID {
		t.Errorf("restored.ID = %d, want %d (same row, undeleted)", restored.ID, originalID)
	}
	if !restored.DeletedAt.Time.IsZero() && restored.DeletedAt.Valid {
		t.Errorf("restored.DeletedAt still set: %+v", restored.DeletedAt)
	}

	// User-edited fields survived the resurrect.
	if restored.Notes == nil || *restored.Notes != priorNotes {
		t.Errorf("restored notes = %v, want preserved", restored.Notes)
	}
	if restored.CategoryID == nil || *restored.CategoryID != cat.ID {
		t.Errorf("restored category_id = %v, want %d", restored.CategoryID, cat.ID)
	}

	// Plaid-owned fields reflect the current payload (amount/description
	// re-applied — even if upstream Plaid hadn't changed them, the merge
	// is a no-op).
	if !restored.Amount.Equal(decimal.NewFromFloat(-3.14)) {
		t.Errorf("restored amount = %s, want -3.14", restored.Amount)
	}
}
