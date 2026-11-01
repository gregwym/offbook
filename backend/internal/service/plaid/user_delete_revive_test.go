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

// #696: a bank-synced row the user deleted must not silently reappear. Policy
// (b): when the bank sends it again (pending -> posted under a new ID, or the
// same ID re-added), the row comes back once, unreviewed, stamped with
// restored_after_user_delete_at so Needs review explains why.
func TestService_SyncTransactions_UserDeletedRowReturnsUnreviewed(t *testing.T) {
	cases := []struct {
		name string
		// drain2 is what Plaid sends after the user deleted "ptx-a".
		drain2   func(acct string) map[string]any
		wantPTID string
	}{
		{
			name: "pending deleted then posted under new id",
			drain2: func(acct string) map[string]any {
				return map[string]any{
					"added": []map[string]any{{
						"transaction_id": "ptx-b", "pending_transaction_id": "ptx-a", "account_id": acct,
						"amount": 46.00, "iso_currency_code": "USD", "name": "Corner Cafe", "date": "2026-10-11", "pending": false,
					}},
					"removed": []map[string]any{{"transaction_id": "ptx-a"}},
				}
			},
			wantPTID: "ptx-b",
		},
		{
			name: "deleted row re-added under same id",
			drain2: func(acct string) map[string]any {
				return map[string]any{
					"added": []map[string]any{{
						"transaction_id": "ptx-a", "account_id": acct,
						"amount": 46.00, "iso_currency_code": "USD", "name": "Corner Cafe", "date": "2026-10-11", "pending": false,
					}},
				}
			},
			wantPTID: "ptx-a",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := openPlaidTestDB(t)
			userID := seedPlaidTestUser(t, g)
			plaidAcctID := "pacct-udel-" + string(rune('a'+i))
			itemID := "item-udel-" + string(rune('a'+i))
			acct := &model.Account{
				UserID: userID, Name: "User Delete Checking", InstitutionSlug: "ins_test",
				AccountType: "checking", Currency: "USD",
				PlaidAccountID: &plaidAcctID, IsActive: true,
			}
			if err := g.Create(acct).Error; err != nil {
				t.Fatalf("seed account: %v", err)
			}
			t.Cleanup(func() {
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
						"transaction_id": "ptx-a", "account_id": plaidAcctID, "amount": 40.00,
						"iso_currency_code": "USD", "name": "Corner Cafe", "date": "2026-10-10", "pending": true,
					}}
					resp["next_cursor"] = "c1"
				case 2:
					for k, v := range tc.drain2(plaidAcctID) {
						resp[k] = v
					}
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
				UserID: userID, PlaidItemID: itemID, AccessTokenEnc: enc, Status: "active",
			}); err != nil {
				t.Fatalf("seed item: %v", err)
			}
			svc := plaidsvc.NewService(client, box, itemRepo, acctRepo, txRepo, repository.NewPlaidSyncErrorRepository(g), repository.NewAssetRepository(g), repository.NewPositionRepository(g), piiSvc, nil, g)

			if _, err := svc.SyncTransactions(context.Background(), userID, itemID); err != nil {
				t.Fatalf("sync #1: %v", err)
			}
			var first model.Transaction
			if err := g.Where("user_id = ? AND plaid_transaction_id = ?", userID, "ptx-a").First(&first).Error; err != nil {
				t.Fatalf("fetch: %v", err)
			}
			// The user reviewed it, then deleted it from the Transactions page.
			if err := g.Model(&model.Transaction{}).Where("id = ?", first.ID).
				Update("reviewed_at", time.Now().UTC()).Error; err != nil {
				t.Fatalf("review: %v", err)
			}
			if err := txRepo.SoftDelete(context.Background(), userID, first.ID); err != nil {
				t.Fatalf("user delete: %v", err)
			}
			var deleted model.Transaction
			if err := g.Unscoped().First(&deleted, first.ID).Error; err != nil {
				t.Fatalf("fetch deleted: %v", err)
			}
			if deleted.DeletedBy == nil || *deleted.DeletedBy != model.DeletedByUser {
				t.Fatalf("deleted_by = %v, want %q", deleted.DeletedBy, model.DeletedByUser)
			}

			if _, err := svc.SyncTransactions(context.Background(), userID, itemID); err != nil {
				t.Fatalf("sync #2: %v", err)
			}

			var rows []model.Transaction
			if err := g.Where("user_id = ? AND source = 'plaid'", userID).Find(&rows).Error; err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("live plaid rows = %d, want exactly 1", len(rows))
			}
			got := rows[0]
			if got.ID != first.ID {
				t.Errorf("row id = %d, want the user-deleted row %d revived (no duplicate)", got.ID, first.ID)
			}
			if got.PlaidTransactionID == nil || *got.PlaidTransactionID != tc.wantPTID {
				t.Errorf("plaid_transaction_id = %v, want %s", got.PlaidTransactionID, tc.wantPTID)
			}
			if got.ReviewedAt != nil {
				t.Error("reviewed_at kept: a row the user deleted must come back into Needs review")
			}
			if got.RestoredAfterUserDeleteAt == nil {
				t.Error("restored_after_user_delete_at not set: the UI can't explain why the row came back")
			}
			if got.DeletedBy != nil {
				t.Errorf("deleted_by = %v on a live row, want nil", *got.DeletedBy)
			}
			var sum decimal.Decimal
			if err := g.Model(&model.Transaction{}).Where("user_id = ? AND account_id = ?", userID, acct.ID).
				Select("COALESCE(SUM(amount), 0)").Scan(&sum).Error; err != nil {
				t.Fatalf("sum: %v", err)
			}
			if !sum.Equal(decimal.RequireFromString("-46")) {
				t.Errorf("ledger sum = %s, want exactly -46 (posted amount once)", sum)
			}

			// Idempotent: a third drain changes nothing.
			if _, err := svc.SyncTransactions(context.Background(), userID, itemID); err != nil {
				t.Fatalf("sync #3: %v", err)
			}
			var n int64
			g.Model(&model.Transaction{}).Where("user_id = ? AND source = 'plaid'", userID).Count(&n)
			if n != 1 {
				t.Errorf("rows after resync = %d, want 1", n)
			}
		})
	}
}
