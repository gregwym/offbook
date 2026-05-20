package plaid_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// fakeServerPFC returns a single-page response where the same Plaid txn
// appears in `added` on the first call and in `modified` on the second.
// The modified payload keeps the same PFC. Lets us drive the
// initial-default-categorize then re-sync flow in one test.
func fakeServerPFC(t *testing.T, plaidAcctID, plaidTxnID string) (*httptest.Server, *int) {
	t.Helper()
	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		base := map[string]any{
			"transaction_id":    plaidTxnID,
			"account_id":        plaidAcctID,
			"amount":            12.34,
			"iso_currency_code": "USD",
			"name":              "WHOLE FOODS",
			"merchant_name":     "Whole Foods",
			"date":              "2026-05-10",
			"pending":           false,
			// detailed is the wire form Plaid actually returns: the
			// primary token is repeated as a prefix
			// ("FOOD_AND_DRINK_GROCERIES", not "GROCERIES"). #181 fallout —
			// 000005 seeded the un-prefixed legacy form, 000012 normalized.
			"personal_finance_category": map[string]any{
				"primary":  "FOOD_AND_DRINK",
				"detailed": "FOOD_AND_DRINK_GROCERIES",
			},
		}
		var added, modified []map[string]any
		if calls == 1 {
			added = []map[string]any{base}
		} else {
			// Second call: same Plaid txn comes through as a `modified`
			// (e.g., merchant_name cleaned up by Plaid). PFC unchanged.
			modified = []map[string]any{base}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added":       added,
			"modified":    modified,
			"removed":     []any{},
			"next_cursor": "next",
			"has_more":    false,
			"request_id":  "req",
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

// TestPlaidSync_CategoryMappingFirstPass_AndPreservesUserChoiceOnReSync:
//
// Acceptance criteria from #64:
//   - First sync applies the Plaid PFC → category default
//   - User then re-categorizes the row (writes a different category_id)
//   - Second sync (same txn comes through as `modified`) MUST NOT overwrite
//     the user's choice
func TestPlaidSync_CategoryMappingFirstPass_AndPreservesUserChoiceOnReSync(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)

	// Seed two distinct categories: the Plaid default (groceries) and the
	// one the user will re-classify into (entertainment, picked because
	// it's nothing FOOD_AND_DRINK/GROCERIES would ever map to so the
	// assertion is unambiguous).
	var groceries, entertainment model.Category
	if err := g.Where("slug = ?", "groceries").First(&groceries).Error; err != nil {
		t.Fatalf("groceries category: %v", err)
	}
	if err := g.Where("slug = ?", "entertainment").First(&entertainment).Error; err != nil {
		t.Fatalf("entertainment category: %v", err)
	}

	const plaidAcctID = "pacct-pfc-1"
	const plaidTxnID = "ptx-pfc-1"
	acct := &model.Account{
		UserID: userID, Name: "PFC Acct", InstitutionSlug: "ins_test",
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

	srv, _ := fakeServerPFC(t, plaidAcctID, plaidTxnID)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))

	// Real mapper loaded from the seeded plaid_category_map table.
	mapper, err := plaidsvc.NewCategoryMapper(context.Background(),
		repository.NewPlaidCategoryMapRepository(g))
	if err != nil {
		t.Fatalf("NewCategoryMapper: %v", err)
	}
	if mapper.Size() == 0 {
		t.Fatal("plaid_category_map appears empty — migration 000005 didn't seed?")
	}

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{UserID: userID, PlaidItemID: "item-pfc", AccessTokenEnc: enc, Status: "active"}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), piiSvc, mapper, g)

	// First sync: row is created with the Plaid default category.
	if _, err := svc.SyncTransactions(context.Background(), userID, "item-pfc"); err != nil {
		t.Fatalf("sync #1: %v", err)
	}
	var afterFirst model.Transaction
	if err := g.Where("plaid_transaction_id = ?", plaidTxnID).First(&afterFirst).Error; err != nil {
		t.Fatalf("fetch after sync #1: %v", err)
	}
	if afterFirst.CategoryID == nil || *afterFirst.CategoryID != groceries.ID {
		t.Errorf("first sync category = %v, want groceries (%d)", afterFirst.CategoryID, groceries.ID)
	}
	if afterFirst.CategorizationMethod == nil || *afterFirst.CategorizationMethod != "plaid_default" {
		t.Errorf("categorization_method = %v, want plaid_default", afterFirst.CategorizationMethod)
	}

	// User reclassifies — same write the UI would issue.
	if err := g.Model(&model.Transaction{}).
		Where("id = ?", afterFirst.ID).
		Updates(map[string]any{
			"category_id":           entertainment.ID,
			"categorization_method": "manual",
		}).Error; err != nil {
		t.Fatalf("user reclassify: %v", err)
	}

	// Second sync: same Plaid PFC, but the same txn now arrives as `modified`.
	// MergePlaidUpdate must NOT overwrite the user's category.
	if _, err := svc.SyncTransactions(context.Background(), userID, "item-pfc"); err != nil {
		t.Fatalf("sync #2: %v", err)
	}
	var afterSecond model.Transaction
	if err := g.Where("plaid_transaction_id = ?", plaidTxnID).First(&afterSecond).Error; err != nil {
		t.Fatalf("fetch after sync #2: %v", err)
	}
	if afterSecond.CategoryID == nil || *afterSecond.CategoryID != entertainment.ID {
		t.Errorf("after re-sync category = %v, want entertainment (%d) — user choice clobbered",
			afterSecond.CategoryID, entertainment.ID)
	}
	if afterSecond.CategorizationMethod == nil || *afterSecond.CategorizationMethod != "manual" {
		t.Errorf("after re-sync categorization_method = %v, want manual — should not revert to plaid_default",
			afterSecond.CategorizationMethod)
	}
}

// TestPlaidSync_NoMappingLeavesCategoryNull: when the PFC doesn't resolve
// (and the row arrives with no PFC at all, e.g., legacy/unclassified),
// CategoryID + CategorizationMethod are both nil. Captures the "manual
// re-categorization always wins; this is just a sane default" contract.
func TestPlaidSync_NoMappingLeavesCategoryNull(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)

	const plaidAcctID = "pacct-no-pfc"
	const plaidTxnID = "ptx-no-pfc"
	acct := &model.Account{
		UserID: userID, Name: "NoPFC Acct", InstitutionSlug: "ins_test",
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    plaidTxnID,
					"account_id":        plaidAcctID,
					"amount":            1.00,
					"iso_currency_code": "USD",
					"name":              "Mystery",
					"date":              "2026-05-10",
					"pending":           false,
					// No personal_finance_category at all.
				},
			},
			"modified":    []any{},
			"removed":     []any{},
			"next_cursor": "n",
			"has_more":    false,
			"request_id":  "r",
		})
	}))
	t.Cleanup(srv.Close)

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(acctRepo))
	mapper, _ := plaidsvc.NewCategoryMapper(context.Background(), repository.NewPlaidCategoryMapRepository(g))

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{UserID: userID, PlaidItemID: "item-no-pfc", AccessTokenEnc: enc, Status: "active"}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), piiSvc, mapper, g)
	if _, err := svc.SyncTransactions(context.Background(), userID, "item-no-pfc"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var got model.Transaction
	if err := g.Where("plaid_transaction_id = ?", plaidTxnID).First(&got).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.CategoryID != nil {
		t.Errorf("category_id = %v, want nil (no mapping resolved)", got.CategoryID)
	}
	if got.CategorizationMethod != nil {
		t.Errorf("categorization_method = %v, want nil", got.CategorizationMethod)
	}
}
