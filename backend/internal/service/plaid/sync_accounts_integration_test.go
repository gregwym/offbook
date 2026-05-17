package plaid_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// loadRepoDotenvForPlaid mirrors the helper used by repo/model integration
// tests — `go test` runs in the package dir so the default ./.env / ../.env
// lookup in config.Load won't find the repo-root file.
func loadRepoDotenvForPlaid() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func openPlaidTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	loadRepoDotenvForPlaid()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no DATABASE_URL set; skipping integration test")
	}
	g, err := db.Open(url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx, g); err != nil {
		t.Skipf("db.Ping: %v; skipping integration test", err)
	}
	return g
}

func seedPlaidTestUser(t *testing.T, g *gorm.DB) int64 {
	t.Helper()
	u := &model.User{
		Email:        fmt.Sprintf("plaid-sync-%d@example.test", time.Now().UnixNano()),
		PasswordHash: "x",
		LastScope:    model.ScopePersonal,
		DefaultScope: model.ScopePersonal,
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		// Cascade-style cleanup: scrub child rows first since FKs don't
		// have ON DELETE CASCADE.
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Account{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.PlaidItem{})
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	return u.ID
}

// fakeAccountsServer stands in for Plaid's /accounts/get + /identity/get.
// Same access_token returns the same payload each call so we can drive
// idempotency assertions.
func fakeAccountsServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": []map[string]any{
				{
					"account_id":    "plaid-acct-checking-1",
					"name":          "Plaid Checking",
					"official_name": "Plaid Gold Standard 0% Interest Checking",
					"type":          "depository",
					"subtype":       "checking",
					"mask":          "0000",
					"balances": map[string]any{
						"current":           110.00,
						"available":         100.00,
						"iso_currency_code": "USD",
					},
				},
				{
					"account_id":    "plaid-acct-savings-1",
					"name":          "Plaid Saving",
					"official_name": "Plaid Silver Standard 0.1% Interest Saving",
					"type":          "depository",
					"subtype":       "savings",
					"mask":          "1111",
					"balances": map[string]any{
						"current":           210.00,
						"available":         200.00,
						"iso_currency_code": "USD",
					},
				},
			},
			"item": map[string]any{
				"item_id":          "item-fake-sync-1",
				"institution_id":   "ins_109508",
				"institution_name": "First Platypus Bank",
			},
			"request_id": "req-sync-1",
		})
	})
	mux.HandleFunc("/identity/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": []map[string]any{
				{
					"account_id": "plaid-acct-checking-1",
					"name":       "Plaid Checking",
					"type":       "depository",
					"subtype":    "checking",
					"balances":   map[string]any{"current": 110.00, "iso_currency_code": "USD"},
					"owners": []map[string]any{
						{
							"names": []string{"Greg PlaidSandbox Owner"},
						},
					},
				},
				{
					"account_id": "plaid-acct-savings-1",
					"name":       "Plaid Saving",
					"type":       "depository",
					"subtype":    "savings",
					"balances":   map[string]any{"current": 210.00, "iso_currency_code": "USD"},
					"owners": []map[string]any{
						{
							"names": []string{"Greg PlaidSandbox Owner", "Joint Coowner Sample"},
						},
					},
				},
			},
			"item":       map[string]any{"item_id": "item-fake-sync-1"},
			"request_id": "req-sync-2",
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

func TestService_SyncAccounts_PIIIsolationAndIdempotency(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	srv := fakeAccountsServer(t)

	client, err := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	box, err := crypto.NewSecretBox(newTestKey())
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	acctSvc := service.NewAccountService(acctRepo)
	piiRepo := repository.NewPIIRepository(g)
	piiSvc := service.NewPIIService(piiRepo, acctSvc)

	// Pre-seed a plaid_items row pointing at our fake institution.
	enc, err := box.Encrypt([]byte("access-sandbox-fake-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	item := &model.PlaidItem{
		UserID:         userID,
		PlaidItemID:    "item-fake-sync-1",
		AccessTokenEnc: enc,
		Status:         "active",
	}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, repository.NewTransactionRepository(g), piiSvc)

	// First sync: 2 accounts created.
	r1, err := svc.SyncAccounts(context.Background(), userID, "item-fake-sync-1")
	if err != nil {
		t.Fatalf("SyncAccounts #1: %v", err)
	}
	if r1.Created != 2 || r1.Updated != 0 {
		t.Errorf("first run: created=%d updated=%d, want 2/0", r1.Created, r1.Updated)
	}

	// Verify holder names landed in pii_store, NOT on accounts.name.
	// The PII tokens that are present only in pii_store should not appear
	// anywhere else (testutil scan covers accounts.name / transactions /
	// ai_messages / categories).
	testutil.AssertNoPIILeak(t, g, []string{
		"Greg PlaidSandbox Owner",
		"Joint Coowner Sample",
	})

	// And confirm the pii_store actually has them.
	var piiCount int64
	if err := g.Model(&model.PIIRecord{}).
		Where("entity_type = 'account' AND field_name = 'holder_name'").
		Count(&piiCount).Error; err != nil {
		t.Fatalf("count pii: %v", err)
	}
	if piiCount < 2 {
		t.Errorf("expected ≥2 holder_name rows in pii_store, got %d", piiCount)
	}

	// Idempotency: second run should produce 0 created.
	r2, err := svc.SyncAccounts(context.Background(), userID, "item-fake-sync-1")
	if err != nil {
		t.Fatalf("SyncAccounts #2: %v", err)
	}
	if r2.Created != 0 {
		t.Errorf("second run: created=%d, want 0", r2.Created)
	}
	if r2.Updated != 2 {
		t.Errorf("second run: updated=%d, want 2", r2.Updated)
	}

	// No duplicate accounts rows for either Plaid account ID.
	for _, plaidID := range []string{"plaid-acct-checking-1", "plaid-acct-savings-1"} {
		var n int64
		if err := g.Model(&model.Account{}).
			Where("user_id = ? AND plaid_account_id = ?", userID, plaidID).
			Count(&n).Error; err != nil {
			t.Fatalf("count accounts: %v", err)
		}
		if n != 1 {
			t.Errorf("plaid_account_id=%s: %d rows, want 1", plaidID, n)
		}
	}

	// No duplicate pii_store rows on second pass either — the (entity_type,
	// entity_id, field_name) unique constraint + Set's upsert keep this clean.
	var piiCount2 int64
	if err := g.Model(&model.PIIRecord{}).
		Where("entity_type = 'account' AND field_name = 'holder_name'").
		Count(&piiCount2).Error; err != nil {
		t.Fatalf("count pii #2: %v", err)
	}
	if piiCount2 != piiCount {
		t.Errorf("pii_store grew between runs: %d → %d", piiCount, piiCount2)
	}

	// Account type mapping landed correctly.
	var checking model.Account
	if err := g.Where("user_id = ? AND plaid_account_id = ?", userID, "plaid-acct-checking-1").
		First(&checking).Error; err != nil {
		t.Fatalf("checking acct: %v", err)
	}
	if checking.AccountType != "checking" {
		t.Errorf("checking acct: type=%q, want checking", checking.AccountType)
	}
	if checking.Name != "Plaid Gold Standard 0% Interest Checking" {
		t.Errorf("checking acct: name=%q (should prefer official_name)", checking.Name)
	}
	if checking.LastFour == nil || *checking.LastFour != "0000" {
		t.Errorf("checking acct: last_four=%v, want 0000", checking.LastFour)
	}
}

func TestService_SyncAccounts_ItemNotFound(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)
	srv := fakeAccountsServer(t)

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	acctSvc := service.NewAccountService(acctRepo)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), acctSvc)
	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, repository.NewTransactionRepository(g), piiSvc)

	_, err := svc.SyncAccounts(context.Background(), userID, "nonexistent-item")
	if err != plaidsvc.ErrItemNotFound {
		t.Fatalf("got %v, want ErrItemNotFound", err)
	}
}
