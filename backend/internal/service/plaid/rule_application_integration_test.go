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

// TestPlaidSync_RuleWinsOverPlaidDefault: a Plaid txn with a PFC that would
// normally map to groceries gets routed to the rule's target category
// (entertainment here) because the user has a matching rule.
func TestPlaidSync_RuleWinsOverPlaidDefault(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)

	var groceries, entertainment model.Category
	if err := g.Where("slug = ?", "groceries").First(&groceries).Error; err != nil {
		t.Fatalf("groceries category: %v", err)
	}
	if err := g.Where("slug = ?", "entertainment").First(&entertainment).Error; err != nil {
		t.Fatalf("entertainment category: %v", err)
	}

	const plaidAcctID = "pacct-rule-1"
	const plaidTxnID = "ptx-rule-1"
	acct := &model.Account{
		UserID: userID, Name: "Rule Acct", InstitutionSlug: "ins_test",
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

	// User-owned rule: WHOLE FOODS description → entertainment.
	rule := &model.CategorizationRule{
		UserID: userID, Pattern: "WHOLE FOODS", MatchType: "contains",
		CategoryID: entertainment.ID, Priority: 50, IsActive: true,
	}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	// Same fake server as the PFC test: one `added` with FOOD_AND_DRINK/GROCERIES PFC.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    plaidTxnID,
					"account_id":        plaidAcctID,
					"amount":            12.34,
					"iso_currency_code": "USD",
					"name":              "WHOLE FOODS",
					"merchant_name":     "Whole Foods",
					"date":              "2026-05-10",
					"pending":           false,
					"personal_finance_category": map[string]any{
						"primary":  "FOOD_AND_DRINK",
						"detailed": "FOOD_AND_DRINK_GROCERIES",
					},
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
	mapper, err := plaidsvc.NewCategoryMapper(context.Background(),
		repository.NewPlaidCategoryMapRepository(g))
	if err != nil {
		t.Fatalf("NewCategoryMapper: %v", err)
	}

	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	item := &model.PlaidItem{UserID: userID, PlaidItemID: "item-rule", AccessTokenEnc: enc, Status: "active"}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo,
		repository.NewPlaidSyncErrorRepository(g), piiSvc, mapper, g).
		WithRuleRepo(repository.NewCategorizationRuleRepository(g))

	if _, err := svc.SyncTransactions(context.Background(), userID, "item-rule"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var got model.Transaction
	if err := g.Where("plaid_transaction_id = ?", plaidTxnID).First(&got).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != entertainment.ID {
		t.Errorf("category = %v, want entertainment (%d) — rule should win over plaid_default",
			got.CategoryID, entertainment.ID)
	}
	if got.CategorizationMethod == nil || *got.CategorizationMethod != "rule" {
		t.Errorf("method = %v, want rule", got.CategorizationMethod)
	}
	if got.CategorizationRuleID == nil || *got.CategorizationRuleID != rule.ID {
		t.Errorf("rule_id = %v, want %d", got.CategorizationRuleID, rule.ID)
	}
}

// TestPlaidSync_OtherUsersRuleDoesNotApply: user B's rule must not
// categorize user A's Plaid transaction. Verifies the per-user rule load.
func TestPlaidSync_OtherUsersRuleDoesNotApply(t *testing.T) {
	g := openPlaidTestDB(t)
	userA := seedPlaidTestUser(t, g)
	userB := seedPlaidTestUser(t, g)

	var groceries, entertainment model.Category
	if err := g.Where("slug = ?", "groceries").First(&groceries).Error; err != nil {
		t.Fatalf("groceries: %v", err)
	}
	if err := g.Where("slug = ?", "entertainment").First(&entertainment).Error; err != nil {
		t.Fatalf("entertainment: %v", err)
	}

	const plaidAcctID = "pacct-rule-iso"
	const plaidTxnID = "ptx-rule-iso"
	acct := &model.Account{
		UserID: userA, Name: "Iso Acct", InstitutionSlug: "ins_test",
		AccountType: "checking", Currency: "USD",
		PlaidAccountID: ptr(plaidAcctID), IsActive: true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userA).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
	})

	// Rule belongs to userB but targets the same pattern.
	rule := &model.CategorizationRule{
		UserID: userB, Pattern: "WHOLE FOODS", MatchType: "contains",
		CategoryID: entertainment.ID, Priority: 50, IsActive: true,
	}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": []map[string]any{
				{
					"transaction_id":    plaidTxnID,
					"account_id":        plaidAcctID,
					"amount":            12.34,
					"iso_currency_code": "USD",
					"name":              "WHOLE FOODS",
					"date":              "2026-05-10",
					"pending":           false,
					"personal_finance_category": map[string]any{
						"primary":  "FOOD_AND_DRINK",
						"detailed": "FOOD_AND_DRINK_GROCERIES",
					},
				},
			},
			"modified": []any{}, "removed": []any{},
			"next_cursor": "n", "has_more": false, "request_id": "r",
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
	item := &model.PlaidItem{UserID: userA, PlaidItemID: "item-iso", AccessTokenEnc: enc, Status: "active"}
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo,
		repository.NewPlaidSyncErrorRepository(g), piiSvc, mapper, g).
		WithRuleRepo(repository.NewCategorizationRuleRepository(g))

	if _, err := svc.SyncTransactions(context.Background(), userA, "item-iso"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var got model.Transaction
	if err := g.Where("plaid_transaction_id = ?", plaidTxnID).First(&got).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// Should fall through to plaid_default (groceries), NOT entertainment.
	if got.CategoryID == nil || *got.CategoryID != groceries.ID {
		t.Errorf("category = %v, want groceries (%d) — userB's rule must not apply",
			got.CategoryID, groceries.ID)
	}
	if got.CategorizationMethod == nil || *got.CategorizationMethod != "plaid_default" {
		t.Errorf("method = %v, want plaid_default", got.CategorizationMethod)
	}
	if got.CategorizationRuleID != nil {
		t.Errorf("rule_id = %v, want nil (no rule matched)", got.CategorizationRuleID)
	}
}
