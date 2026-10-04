package plaid_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
	"github.com/gregwym/offbook/backend/internal/testutil"
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
	// #369: the scheduler now calls SyncAccounts before SyncTransactions for
	// every item, so every scheduler test needs /accounts/get to resolve.
	// Echo back the single account the test already seeded (keyed by
	// access_token) with a flat balance — these tests assert on the
	// transactions pass, not account/balance content.
	mux.HandleFunc("/accounts/get", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		token, _ := body["access_token"].(string)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": []map[string]any{
				{
					"account_id": keyedAccountID(token),
					"name":       "Scheduler Test Checking",
					"type":       "depository",
					"subtype":    "checking",
					"mask":       "0000",
					"balances": map[string]any{
						"current":           0,
						"iso_currency_code": "USD",
					},
				},
			},
			"item":       map[string]any{"item_id": "item-" + token},
			"request_id": "req-accts-" + token,
		})
	})
	mux.HandleFunc("/identity/get", func(w http.ResponseWriter, r *http.Request) {
		// Best-effort in the real client — a non-200 here is swallowed.
		http.Error(w, "identity not supported", http.StatusBadRequest)
	})
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
		g.Unscoped().Where("user_id IN ?", []int64{userA, userB}).Delete(&model.AccountBalanceObservation{})
		g.Unscoped().Where("user_id IN ?", []int64{userA, userB}).Delete(&model.Position{})
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
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo, acctRepo).WithJitter(0).WithPause(0)

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
		// kind='flow' isolates the one synced transaction from any
		// opening_balance/adjustment row #369's SyncAccounts-then-
		// SyncTransactions ordering may also write while reconciling the
		// cash position — this assertion is about per-user isolation of
		// synced data, not the reconciliation side effect.
		var count int64
		g.Model(&model.Transaction{}).Where("user_id = ? AND kind = ?", tc.userID, model.KindFlow).Count(&count)
		if count != 1 {
			t.Errorf("user %d has %d flow transactions, want 1 (per-user isolation)", tc.userID, count)
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
				g.Unscoped().Where("user_id = ?", id).Delete(&model.AccountBalanceObservation{})
				g.Unscoped().Where("user_id = ?", id).Delete(&model.Position{})
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
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo, acctRepo).WithJitter(0).WithPause(0)

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
		g.Unscoped().Where("user_id IN ?", []int64{userOK, userFail}).Delete(&model.AccountBalanceObservation{})
		g.Unscoped().Where("user_id IN ?", []int64{userOK, userFail}).Delete(&model.Position{})
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
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo, acctRepo).WithJitter(0).WithPause(0)

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
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo, nil).WithJitter(time.Hour)

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

// schedulerFakeClient implements plaidsvc.Client with per-access_token
// canned FetchAccounts/FetchHoldings responses and a no-op transactions
// sync. Unlike keyedTxnsSyncServer's httptest server, this drives the
// scheduler's Go-level orchestration directly (SyncAccounts →
// SyncTransactions → the #369 holdings gate) without needing to match the
// Plaid SDK's JSON wire format for every method.
type schedulerFakeClient struct {
	accounts map[string]plaidsvc.AccountsResult
	holdings map[string]plaidsvc.HoldingsResult
}

func (f *schedulerFakeClient) CreateLinkToken(context.Context, int64) (plaidsvc.LinkToken, error) {
	return plaidsvc.LinkToken{}, fmt.Errorf("not used in test")
}
func (f *schedulerFakeClient) CreateUpdateLinkToken(context.Context, int64, string) (plaidsvc.LinkToken, error) {
	return plaidsvc.LinkToken{}, fmt.Errorf("not used in test")
}
func (f *schedulerFakeClient) ExchangePublicToken(context.Context, string) (plaidsvc.Item, error) {
	return plaidsvc.Item{}, fmt.Errorf("not used in test")
}
func (f *schedulerFakeClient) FetchAccounts(_ context.Context, accessToken string) (plaidsvc.AccountsResult, error) {
	res, ok := f.accounts[accessToken]
	if !ok {
		return plaidsvc.AccountsResult{}, fmt.Errorf("schedulerFakeClient: no accounts for token %q", accessToken)
	}
	return res, nil
}
func (f *schedulerFakeClient) SyncTransactions(context.Context, string, string) (plaidsvc.SyncTransactionsPage, error) {
	return plaidsvc.SyncTransactionsPage{}, nil
}
func (f *schedulerFakeClient) FetchInvestmentTransactions(context.Context, string, time.Time, time.Time) (plaidsvc.InvestmentTransactionsResult, error) {
	return plaidsvc.InvestmentTransactionsResult{}, nil
}
func (f *schedulerFakeClient) FetchHoldings(_ context.Context, accessToken string) (plaidsvc.HoldingsResult, error) {
	res, ok := f.holdings[accessToken]
	if !ok {
		return plaidsvc.HoldingsResult{}, fmt.Errorf("schedulerFakeClient: no holdings for token %q", accessToken)
	}
	return res, nil
}
func (f *schedulerFakeClient) ResetSandboxItemLogin(context.Context, string) error {
	return fmt.Errorf("not used in test")
}

// TestSyncScheduler_RunOnce_SyncsHoldingsForInvestmentAccounts covers #369's
// acceptance criterion that the daily scheduler extends to holdings for
// investment accounts: a local "investment" account for an item should get
// its /investments/holdings/get snapshot reconciled into positions, same
// pass as the transactions sync.
func TestSyncScheduler_RunOnce_SyncsHoldingsForInvestmentAccounts(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	usdID := testutil.LookupUSDAssetID(t, g)

	// Unique suffix per run — accounts.plaid_account_id and assets.symbol
	// both carry global unique constraints, and this test DB is a shared,
	// long-lived fixture (not torn down between runs).
	suffix := time.Now().Format("150405.000000000")
	plaidAcctID := "pacct-sched-holdings-" + suffix
	accessToken := "access-token-holdings-" + suffix
	plaidItemID := "item-sched-holdings-" + suffix
	securityID := "sec-aapl-sched-" + suffix
	tickerSymbol := "AAPL-SCHED-" + suffix

	acct := &model.Account{
		UserID: userID, Name: "Brokerage", InstitutionSlug: "ins_test",
		AccountType: "investment", Currency: "USD", PrimaryQuoteAssetID: usdID,
		PlaidAccountID: strp(plaidAcctID), IsActive: true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acct.ID).Delete(&model.Position{})
		g.Unscoped().Where("account_id = ?", acct.ID).Delete(&model.Transaction{})
		g.Unscoped().Where("account_id = ?", acct.ID).Delete(&model.AccountBalanceObservation{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
	})

	client := &schedulerFakeClient{
		accounts: map[string]plaidsvc.AccountsResult{
			accessToken: {
				Accounts: []plaidsvc.DiscoveredAccount{
					{
						PlaidAccountID: plaidAcctID,
						Name:           "Brokerage",
						Type:           "investment",
						Subtype:        "brokerage",
						Currency:       "USD",
						Balance:        decimal.Zero,
					},
				},
			},
		},
		holdings: map[string]plaidsvc.HoldingsResult{
			accessToken: {
				Holdings: []plaidsvc.PlaidHolding{
					{
						PlaidAccountID:   plaidAcctID,
						PlaidSecurityID:  securityID,
						Quantity:         decimal.NewFromInt(10),
						InstitutionPrice: decimal.NewFromInt(200),
						IsoCurrencyCode:  "USD",
					},
				},
				Securities: []plaidsvc.PlaidSecurity{
					{
						PlaidSecurityID: securityID,
						TickerSymbol:    tickerSymbol,
						Name:            "Apple Inc (scheduler test)",
						Type:            "equity",
						IsoCurrencyCode: "USD",
					},
				},
			},
		},
	}
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))

	enc, _ := box.Encrypt([]byte(accessToken))
	item := &model.PlaidItem{UserID: userID, PlaidItemID: plaidItemID, AccessTokenEnc: enc, Status: "active"}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)
	scheduler := plaidsvc.NewSyncScheduler(svc, itemRepo, acctRepo).WithJitter(0).WithPause(0)

	res := scheduler.RunOnce(context.Background())
	if res.Synced != 1 || res.Failed != 0 || res.HoldingsFailed != 0 {
		t.Fatalf("RunOnce = %+v, want {Synced:1 Failed:0 HoldingsFailed:0}", res)
	}

	var asset model.Asset
	if err := g.Where("symbol = ?", tickerSymbol).First(&asset).Error; err != nil {
		t.Fatalf("expected %s asset to be created: %v", tickerSymbol, err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, asset.ID) })

	var pos model.Position
	if err := g.Where("account_id = ? AND asset_id = ?", acct.ID, asset.ID).First(&pos).Error; err != nil {
		t.Fatalf("expected a reconciled %s position, got: %v", tickerSymbol, err)
	}
	if !pos.Quantity.Equal(decimal.NewFromInt(10)) {
		t.Errorf("position quantity = %s, want 10", pos.Quantity.String())
	}
}

func strp(s string) *string { return &s }
