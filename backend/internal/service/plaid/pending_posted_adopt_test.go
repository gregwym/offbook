package plaid_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// Plaid often delivers pending→posted as remove(pending id) + add(posted id,
// pending_transaction_id=pending id). The posted row must adopt the pending
// row so notes, review state and splits survive, with no duplicate.
func TestService_SyncTransactions_PostedAdoptsPendingRow(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)

	const plaidAcctID = "pacct-adopt-1"
	acct := &model.Account{
		UserID: userID, Name: "Adopt Checking", InstitutionSlug: "ins_test",
		AccountType: "checking", Currency: "USD",
		PlaidAccountID: &[]string{plaidAcctID}[0], IsActive: true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.TransactionSplit{})
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
	})

	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"added": []any{}, "modified": []any{}, "removed": []any{}, "has_more": false, "request_id": "r"}
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			resp["added"] = []map[string]any{{
				"transaction_id": "ptx-pending", "account_id": plaidAcctID, "amount": 40.00,
				"iso_currency_code": "USD", "name": "Corner Cafe", "date": "2026-10-10", "pending": true,
			}}
			resp["next_cursor"] = "c1"
		case 2:
			resp["added"] = []map[string]any{{
				"transaction_id": "ptx-posted", "pending_transaction_id": "ptx-pending", "account_id": plaidAcctID,
				"amount": 46.00, "iso_currency_code": "USD", "name": "Corner Cafe", "date": "2026-10-11", "pending": false,
			}}
			resp["removed"] = []map[string]any{{"transaction_id": "ptx-pending"}}
			resp["next_cursor"] = "c2"
		default:
			resp["next_cursor"] = "c2"
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))
	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	if err := itemRepo.Create(context.Background(), &model.PlaidItem{
		UserID: userID, PlaidItemID: "item-adopt-1", AccessTokenEnc: enc, Status: "active",
	}); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)

	if _, err := svc.SyncTransactions(context.Background(), userID, "item-adopt-1"); err != nil {
		t.Fatalf("sync #1: %v", err)
	}

	// User edits the pending row: category, note, reviewed, hidden, and a
	// two-way split.
	var pending model.Transaction
	if err := g.Where("user_id = ? AND plaid_transaction_id = ?", userID, "ptx-pending").First(&pending).Error; err != nil {
		t.Fatalf("fetch pending: %v", err)
	}
	if !pending.Pending {
		t.Fatal("pending = false after syncing a Plaid pending row (#513)")
	}
	var cat model.Category
	if err := g.Where("slug = ?", "groceries").First(&cat).Error; err != nil {
		t.Fatalf("seeded category: %v", err)
	}
	now := time.Now().UTC()
	note := "team lunch"
	if err := g.Model(&model.Transaction{}).Where("id = ?", pending.ID).
		Updates(map[string]any{
			"notes": note, "reviewed_at": now, "hidden_at": now,
			"category_id": cat.ID, "categorization_method": "manual",
		}).Error; err != nil {
		t.Fatalf("edit pending: %v", err)
	}
	for _, amt := range []string{"-25", "-15"} {
		d, _ := decimal.NewFromString(amt)
		if err := g.Create(&model.TransactionSplit{TransactionID: pending.ID, UserID: userID, Amount: d}).Error; err != nil {
			t.Fatalf("seed split: %v", err)
		}
	}

	if _, err := svc.SyncTransactions(context.Background(), userID, "item-adopt-1"); err != nil {
		t.Fatalf("sync #2: %v", err)
	}

	var rows []model.Transaction
	if err := g.Where("user_id = ? AND source = 'plaid'", userID).Find(&rows).Error; err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("live plaid rows = %d, want 1 (adopted, no duplicate)", len(rows))
	}
	got := rows[0]
	if got.ID != pending.ID {
		t.Errorf("row id = %d, want adopted pending id %d", got.ID, pending.ID)
	}
	if got.PlaidTransactionID == nil || *got.PlaidTransactionID != "ptx-posted" {
		t.Errorf("plaid_transaction_id = %v, want ptx-posted", got.PlaidTransactionID)
	}
	if !got.Amount.Equal(decimal.NewFromInt(-46)) {
		t.Errorf("amount = %s, want -46 (bank-posted)", got.Amount)
	}
	if got.Notes == nil || *got.Notes != note {
		t.Errorf("notes = %v, want preserved", got.Notes)
	}
	if got.ReviewedAt == nil {
		t.Error("reviewed_at lost across pending→posted")
	}
	if got.Pending {
		t.Error("pending still true after the posted replacement adopted the row (#513)")
	}
	if got.CategoryID == nil || *got.CategoryID != cat.ID {
		t.Errorf("category_id = %v, want user's pick %d preserved", got.CategoryID, cat.ID)
	}
	if got.HiddenAt == nil {
		t.Error("hidden_at lost across pending→posted")
	}
	// Exactly-once financial effect: the ledger holds the posted amount once.
	var sum decimal.Decimal
	if err := g.Model(&model.Transaction{}).Where("user_id = ? AND account_id = ?", userID, acct.ID).
		Select("COALESCE(SUM(amount), 0)").Scan(&sum).Error; err != nil {
		t.Fatalf("sum: %v", err)
	}
	if !sum.Equal(decimal.RequireFromString("-46")) {
		t.Errorf("ledger sum = %s, want exactly -46 (no pending+posted double count)", sum)
	}
	var nSplits int64
	g.Model(&model.TransactionSplit{}).Where("transaction_id = ?", got.ID).Count(&nSplits)
	if nSplits != 2 {
		t.Errorf("splits = %d, want 2 preserved", nSplits)
	}

	// Third sync is a no-op (idempotent).
	if _, err := svc.SyncTransactions(context.Background(), userID, "item-adopt-1"); err != nil {
		t.Fatalf("sync #3: %v", err)
	}
	var n int64
	g.Model(&model.Transaction{}).Where("user_id = ? AND source = 'plaid'", userID).Count(&n)
	if n != 1 {
		t.Errorf("rows after idempotent resync = %d, want 1", n)
	}
}

// #690: Plaid can remove the pending ID in one drain and add its posted
// replacement in a later one. The soft-deleted pending row must still be
// adopted, brought back live under the posted ID, with every user edit kept
// and the posted amount counted exactly once.
func TestService_SyncTransactions_PostedAdoptsPendingRemovedInEarlierSync(t *testing.T) {
	g := openPlaidTestDB(t)
	userID := seedPlaidTestUser(t, g)

	const plaidAcctID = "pacct-adopt-2"
	acct := &model.Account{
		UserID: userID, Name: "Adopt Later Checking", InstitutionSlug: "ins_test",
		AccountType: "checking", Currency: "USD",
		PlaidAccountID: &[]string{plaidAcctID}[0], IsActive: true,
	}
	if err := g.Create(acct).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.TransactionSplit{})
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acct.ID)
	})

	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"added": []any{}, "modified": []any{}, "removed": []any{}, "has_more": false, "request_id": "r"}
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			resp["added"] = []map[string]any{{
				"transaction_id": "ptx2-pending", "account_id": plaidAcctID, "amount": 40.00,
				"iso_currency_code": "USD", "name": "Corner Cafe", "date": "2026-10-10", "pending": true,
			}}
			resp["next_cursor"] = "c1"
		case 2:
			resp["removed"] = []map[string]any{{"transaction_id": "ptx2-pending"}}
			resp["next_cursor"] = "c2"
		case 3:
			resp["added"] = []map[string]any{{
				"transaction_id": "ptx2-posted", "pending_transaction_id": "ptx2-pending", "account_id": plaidAcctID,
				"amount": 40.00, "iso_currency_code": "USD", "name": "Corner Cafe", "date": "2026-10-11", "pending": false,
			}}
			resp["next_cursor"] = "c3"
		default:
			resp["next_cursor"] = "c3"
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	itemRepo := repository.NewPlaidItemRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	piiSvc := service.NewPIIService(repository.NewPIIRepository(g), service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)))
	enc, _ := box.Encrypt([]byte("access-sandbox-fake"))
	if err := itemRepo.Create(context.Background(), &model.PlaidItem{
		UserID: userID, PlaidItemID: "item-adopt-2", AccessTokenEnc: enc, Status: "active",
	}); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)
	sync := func(n int) {
		t.Helper()
		if _, err := svc.SyncTransactions(context.Background(), userID, "item-adopt-2"); err != nil {
			t.Fatalf("sync #%d: %v", n, err)
		}
	}

	sync(1)
	var pending model.Transaction
	if err := g.Where("user_id = ? AND plaid_transaction_id = ?", userID, "ptx2-pending").First(&pending).Error; err != nil {
		t.Fatalf("fetch pending: %v", err)
	}
	var cat model.Category
	if err := g.Where("slug = ?", "groceries").First(&cat).Error; err != nil {
		t.Fatalf("seeded category: %v", err)
	}
	now := time.Now().UTC()
	note := "team lunch"
	if err := g.Model(&model.Transaction{}).Where("id = ?", pending.ID).
		Updates(map[string]any{
			"notes": note, "reviewed_at": now, "hidden_at": now,
			"category_id": cat.ID, "categorization_method": "manual",
		}).Error; err != nil {
		t.Fatalf("edit pending: %v", err)
	}
	for _, amt := range []string{"-25", "-15"} {
		if err := g.Create(&model.TransactionSplit{TransactionID: pending.ID, UserID: userID, Amount: decimal.RequireFromString(amt)}).Error; err != nil {
			t.Fatalf("seed split: %v", err)
		}
	}

	sync(2) // pending removed on its own
	var live int64
	g.Model(&model.Transaction{}).Where("user_id = ? AND source = 'plaid'", userID).Count(&live)
	if live != 0 {
		t.Fatalf("live rows after pending removal = %d, want 0", live)
	}

	sync(3) // posted replacement arrives one drain later

	var rows []model.Transaction
	if err := g.Where("user_id = ? AND source = 'plaid'", userID).Find(&rows).Error; err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("live plaid rows = %d, want 1 (adopted, no duplicate)", len(rows))
	}
	got := rows[0]
	if got.ID != pending.ID {
		t.Errorf("row id = %d, want adopted pending id %d", got.ID, pending.ID)
	}
	if got.PlaidTransactionID == nil || *got.PlaidTransactionID != "ptx2-posted" {
		t.Errorf("plaid_transaction_id = %v, want ptx2-posted", got.PlaidTransactionID)
	}
	if got.Pending {
		t.Error("pending still true after the posted replacement adopted the row")
	}
	if got.Notes == nil || *got.Notes != note {
		t.Errorf("notes = %v, want preserved", got.Notes)
	}
	if got.ReviewedAt == nil {
		t.Error("reviewed_at lost across a split pending→posted drain")
	}
	if got.RestoredAfterUserDeleteAt != nil {
		t.Error("restored_after_user_delete_at set on a row Plaid (not the user) removed (#696)")
	}
	if got.HiddenAt == nil {
		t.Error("hidden_at lost across a split pending→posted drain")
	}
	if got.CategoryID == nil || *got.CategoryID != cat.ID {
		t.Errorf("category_id = %v, want user's pick %d preserved", got.CategoryID, cat.ID)
	}
	var nSplits int64
	g.Model(&model.TransactionSplit{}).Where("transaction_id = ?", got.ID).Count(&nSplits)
	if nSplits != 2 {
		t.Errorf("splits = %d, want 2 preserved", nSplits)
	}
	sumFlows := func() decimal.Decimal {
		var sum decimal.Decimal
		if err := g.Model(&model.Transaction{}).
			Where("user_id = ? AND account_id = ? AND kind = ?", userID, acct.ID, model.KindFlow).
			Select("COALESCE(SUM(amount), 0)").Scan(&sum).Error; err != nil {
			t.Fatalf("sum: %v", err)
		}
		return sum
	}
	if s := sumFlows(); !s.Equal(decimal.RequireFromString("-40")) {
		t.Errorf("flow sum = %s, want exactly -40 (posted amount once)", s)
	}

	sync(4) // idempotent
	var n int64
	g.Model(&model.Transaction{}).Where("user_id = ? AND source = 'plaid'", userID).Count(&n)
	if n != 1 {
		t.Errorf("rows after idempotent resync = %d, want 1", n)
	}
	if s := sumFlows(); !s.Equal(decimal.RequireFromString("-40")) {
		t.Errorf("flow sum after resync = %s, want -40", s)
	}
}
