package plaid_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// keyedAccountID is the Plaid-side account_id a given access token's fake
// transaction is reported against — "pacct-<token>" so every token maps to
// its own globally-unique local account row (accounts.plaid_account_id has
// a global, not per-user, unique index).
func keyedAccountID(token string) string { return "pacct-" + token }

// keyedTxnsSyncServer returns one canned transaction per distinct
// access_token on its first call, empty on subsequent calls, and 500s for
// any access_token in failFor — letting scheduler tests exercise multiple
// items behind a single fake Plaid host (the real Plaid API routes by
// access_token, not URL, so one Service/client serves every item).
func keyedTxnsSyncServer(t *testing.T, failFor map[string]bool) (*httptest.Server, map[string]int) {
	t.Helper()
	var mu sync.Mutex
	calls := map[string]int{}

	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		token, _ := body["access_token"].(string)

		mu.Lock()
		calls[token]++
		n := calls[token]
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if failFor[token] {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error_type": "API_ERROR",
				"error_code": "INTERNAL_SERVER_ERROR",
				"request_id": "req-fail",
			})
			return
		}
		if n == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"added": []map[string]any{
					{
						"transaction_id":    "ptx-" + token,
						"account_id":        keyedAccountID(token),
						"amount":            9.99,
						"iso_currency_code": "USD",
						"name":              "Scheduler Test Txn",
						"date":              "2026-06-01",
						"pending":           false,
					},
				},
				"modified":    []any{},
				"removed":     []any{},
				"next_cursor": "cursor-" + token,
				"has_more":    false,
				"request_id":  "req-" + token,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added":       []any{},
			"modified":    []any{},
			"removed":     []any{},
			"next_cursor": "cursor-" + token,
			"has_more":    false,
			"request_id":  "req-empty-" + token,
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Plaid call: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, calls
}

func TestSyncScheduler_RunOnce_SyncsMultipleItemsAcrossUsers(t *testing.T) {
	g := openPlaidTestDB(t)
	userA := seedPlaidTestUser(t, g)
	userB := seedPlaidTestUser(t, g)

	acctA := &model.Account{UserID: userA, Name: "A Checking", InstitutionSlug: "ins_test", AccountType: "checking", Currency: "USD", PlaidAccountID: strp(keyedAccountID("access-token-a")), IsActive: true}
	acctB := &model.Account{UserID: userB, Name: "B Checking", InstitutionSlug: "ins_test", AccountType: "checking", Currency: "USD", PlaidAccountID: strp(keyedAccountID("access-token-b")), IsActive: true}
	if err := g.Create(acctA).Error; err != nil {
		t.Fatalf("seed account A: %v", err)
	}
	if err := g.Create(acctB).Error; err != nil {
		t.Fatalf("seed account B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id IN ?", []int64{userA, userB}).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acctA.ID)
		g.Unscoped().Delete(&model.Account{}, acctB.ID)
	})

	srv, calls := keyedTxnsSyncServer(t, nil)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))

	encA, _ := box.Encrypt([]byte("access-token-a"))
	encB, _ := box.Encrypt([]byte("access-token-b"))
	itemA := &model.PlaidItem{UserID: userA, PlaidItemID: "item-sched-a", AccessTokenEnc: encA, Status: "active"}
	itemB := &model.PlaidItem{UserID: userB, PlaidItemID: "item-sched-b", AccessTokenEnc: encB, Status: "active"}
	if err := itemRepo.Create(context.Background(), itemA); err != nil {
		t.Fatalf("seed item A: %v", err)
	}
	if err := itemRepo.Create(context.Background(), itemB); err != nil {
		t.Fatalf("seed item B: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo).WithJitter(0).WithPause(0)

	res := scheduler.RunOnce(context.Background())
	if res.Synced != 2 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("RunOnce = %+v, want {Synced:2 Skipped:0 Failed:0}", res)
	}
	if calls["access-token-a"] != 1 || calls["access-token-b"] != 1 {
		t.Errorf("calls = %+v, want exactly 1 call per item", calls)
	}

	for _, tc := range []struct {
		userID int64
		itemID string
	}{{userA, "item-sched-a"}, {userB, "item-sched-b"}} {
		persisted, err := itemRepo.GetByPlaidItemID(context.Background(), tc.userID, tc.itemID)
		if err != nil {
			t.Fatalf("re-fetch %s: %v", tc.itemID, err)
		}
		if persisted.LastSyncStatus != "ok" {
			t.Errorf("%s last_sync_status = %q, want ok", tc.itemID, persisted.LastSyncStatus)
		}
		var count int64
		g.Model(&model.Transaction{}).Where("user_id = ?", tc.userID).Count(&count)
		if count != 1 {
			t.Errorf("user %d has %d transactions, want 1 (per-user isolation)", tc.userID, count)
		}
	}
}

func TestSyncScheduler_RunOnce_SkipsSyncingAndErrorItems(t *testing.T) {
	g := openPlaidTestDB(t)
	userOK := seedPlaidTestUser(t, g)
	userSyncing := seedPlaidTestUser(t, g)
	userError := seedPlaidTestUser(t, g)

	tokensByUser := map[int64]string{
		userOK:      "access-token-ok",
		userSyncing: "access-token-syncing",
		userError:   "access-token-error",
	}
	for _, uid := range []int64{userOK, userSyncing, userError} {
		acct := &model.Account{UserID: uid, Name: "Checking", InstitutionSlug: "ins_test", AccountType: "checking", Currency: "USD", PlaidAccountID: strp(keyedAccountID(tokensByUser[uid])), IsActive: true}
		if err := g.Create(acct).Error; err != nil {
			t.Fatalf("seed account for user %d: %v", uid, err)
		}
		t.Cleanup(func(id int64) func() {
			return func() {
				g.Unscoped().Where("user_id = ?", id).Delete(&model.Transaction{})
				g.Unscoped().Where("user_id = ?", id).Delete(&model.Account{})
			}
		}(uid))
	}

	srv, calls := keyedTxnsSyncServer(t, nil)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))

	encOK, _ := box.Encrypt([]byte("access-token-ok"))
	encSyncing, _ := box.Encrypt([]byte("access-token-syncing"))
	encError, _ := box.Encrypt([]byte("access-token-error"))
	itemOK := &model.PlaidItem{UserID: userOK, PlaidItemID: "item-ok", AccessTokenEnc: encOK, Status: "active"}
	itemSyncing := &model.PlaidItem{UserID: userSyncing, PlaidItemID: "item-syncing", AccessTokenEnc: encSyncing, Status: "active", LastSyncStatus: "syncing"}
	itemError := &model.PlaidItem{UserID: userError, PlaidItemID: "item-error", AccessTokenEnc: encError, Status: "active", LastSyncStatus: "error"}
	for _, it := range []*model.PlaidItem{itemOK, itemSyncing, itemError} {
		if err := itemRepo.Create(context.Background(), it); err != nil {
			t.Fatalf("seed item %s: %v", it.PlaidItemID, err)
		}
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo).WithJitter(0).WithPause(0)

	res := scheduler.RunOnce(context.Background())
	if res.Synced != 1 || res.Skipped != 2 || res.Failed != 0 {
		t.Fatalf("RunOnce = %+v, want {Synced:1 Skipped:2 Failed:0}", res)
	}
	if calls["access-token-syncing"] != 0 || calls["access-token-error"] != 0 {
		t.Errorf("calls = %+v, want the syncing/error items never to reach Plaid", calls)
	}
	if calls["access-token-ok"] != 1 {
		t.Errorf("calls[ok] = %d, want 1", calls["access-token-ok"])
	}
}

func TestSyncScheduler_RunOnce_IsolatesPerItemFailure(t *testing.T) {
	g := openPlaidTestDB(t)
	userOK := seedPlaidTestUser(t, g)
	userFail := seedPlaidTestUser(t, g)

	acctOK := &model.Account{UserID: userOK, Name: "Checking", InstitutionSlug: "ins_test", AccountType: "checking", Currency: "USD", PlaidAccountID: strp(keyedAccountID("access-token-good")), IsActive: true}
	acctFail := &model.Account{UserID: userFail, Name: "Checking", InstitutionSlug: "ins_test", AccountType: "checking", Currency: "USD", PlaidAccountID: strp(keyedAccountID("access-token-fail")), IsActive: true}
	if err := g.Create(acctOK).Error; err != nil {
		t.Fatalf("seed account OK: %v", err)
	}
	if err := g.Create(acctFail).Error; err != nil {
		t.Fatalf("seed account fail: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id IN ?", []int64{userOK, userFail}).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acctOK.ID)
		g.Unscoped().Delete(&model.Account{}, acctFail.ID)
	})

	srv, calls := keyedTxnsSyncServer(t, map[string]bool{"access-token-fail": true})
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))

	encOK, _ := box.Encrypt([]byte("access-token-good"))
	encFail, _ := box.Encrypt([]byte("access-token-fail"))
	itemOK := &model.PlaidItem{UserID: userOK, PlaidItemID: "item-fail-ok", AccessTokenEnc: encOK, Status: "active"}
	itemFail := &model.PlaidItem{UserID: userFail, PlaidItemID: "item-fail-bad", AccessTokenEnc: encFail, Status: "active"}
	if err := itemRepo.Create(context.Background(), itemOK); err != nil {
		t.Fatalf("seed item OK: %v", err)
	}
	if err := itemRepo.Create(context.Background(), itemFail); err != nil {
		t.Fatalf("seed item fail: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo).WithJitter(0).WithPause(0)

	res := scheduler.RunOnce(context.Background())
	if res.Synced != 1 || res.Failed != 1 || res.Skipped != 0 {
		t.Fatalf("RunOnce = %+v, want {Synced:1 Skipped:0 Failed:1}", res)
	}
	if calls["access-token-good"] != 1 || calls["access-token-fail"] != 1 {
		t.Errorf("calls = %+v, want exactly one attempt per item regardless of outcome", calls)
	}

	okItem, err := itemRepo.GetByPlaidItemID(context.Background(), userOK, "item-fail-ok")
	if err != nil {
		t.Fatalf("re-fetch ok item: %v", err)
	}
	if okItem.LastSyncStatus != "ok" {
		t.Errorf("ok item last_sync_status = %q, want ok", okItem.LastSyncStatus)
	}
	failItem, err := itemRepo.GetByPlaidItemID(context.Background(), userFail, "item-fail-bad")
	if err != nil {
		t.Fatalf("re-fetch fail item: %v", err)
	}
	if failItem.LastSyncStatus != "error" {
		t.Errorf("fail item last_sync_status = %q, want error (isolated, doesn't block item-fail-ok)", failItem.LastSyncStatus)
	}
}

func TestSyncScheduler_RunOnce_JitterRespectsContextCancellation(t *testing.T) {
	g := openPlaidTestDB(t)
	itemRepo := repository.NewPlaidItemRepository(g)
	svc := plaidsvc.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo).WithJitter(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	res := scheduler.RunOnce(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("RunOnce blocked %s past a canceled context, want near-instant return", elapsed)
	}
	if res.Synced != 0 || res.Skipped != 0 || res.Failed != 0 {
		t.Errorf("RunOnce on canceled context = %+v, want zero value", res)
	}
}

func strp(s string) *string { return &s }
