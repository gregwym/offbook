package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestTransactionService_BulkEdit covers #567: one category or hide edit over
// the selected rows, user-scoped, with an undo that restores provenance and
// keeps rows edited since.
func TestTransactionService_BulkEdit(t *testing.T) {
	ctx := context.Background()
	svc, userID, accountID, catID, g := newTxSvc(t)

	other := &model.Category{Name: "BulkOther", Slug: "bulk-other-" + time.Now().Format("150405.000000")}
	if err := g.Create(other).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, other.ID) })
	rule := &model.CategorizationRule{UserID: userID, Pattern: "BULK", MatchType: "contains", CategoryID: new(other.ID), Priority: 10, IsActive: true}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })
	// Registered last so it runs first: split lines reference the categories.
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Transaction{}) })

	mk := func(opts ...func(*model.Transaction)) *model.Transaction {
		return mkTxn(t, g, userID, accountID, opts...)
	}
	load := func(id int64) model.Transaction {
		var row model.Transaction
		if err := g.First(&row, id).Error; err != nil {
			t.Fatalf("reload %d: %v", id, err)
		}
		return row
	}
	byRule := func(tx *model.Transaction) {
		m := "rule"
		tx.CategoryID, tx.CategorizationMethod, tx.CategorizationRuleID = new(other.ID), &m, new(rule.ID)
	}
	reviewed := func(tx *model.Transaction) {
		at := time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC)
		tx.ReviewedAt = &at
	}

	t.Run("category applies as a manual pick and undo restores provenance", func(t *testing.T) {
		ruled := mk(byRule, reviewed)
		plaid := mk(withPlaidDefaultCategory(other.ID))
		bare := mk()
		same := mk(withManualCategory(catID))
		adj := mk(func(tx *model.Transaction) { tx.Kind = model.KindAdjustment })
		split := mk(func(tx *model.Transaction) { tx.Amount = decimal.NewFromInt(-30) })
		if err := repository.NewTransactionRepository(g).ReplaceSplits(ctx, userID, split.ID, []model.TransactionSplit{
			{CategoryID: new(other.ID), Amount: decimal.NewFromInt(-10)},
			{CategoryID: new(catID), Amount: decimal.NewFromInt(-20)},
		}); err != nil {
			t.Fatalf("seed splits: %v", err)
		}

		ids := []int64{ruled.ID, plaid.ID, bare.ID, same.ID, adj.ID, split.ID}
		res, err := svc.BulkEdit(ctx, userID, ids, service.BulkEditInput{CategoryID: new(catID)})
		if err != nil {
			t.Fatalf("BulkEdit: %v", err)
		}
		if res.Updated != 3 || res.SkippedSplit != 1 || len(res.Undo) != 3 {
			t.Fatalf("result = %+v, want 3 updated, 1 split skipped", res)
		}
		for _, id := range []int64{ruled.ID, plaid.ID, bare.ID} {
			row := load(id)
			if row.CategoryID == nil || *row.CategoryID != catID || row.CategorizationMethod == nil ||
				*row.CategorizationMethod != "manual" || row.CategorizationRuleID != nil {
				t.Errorf("row %d = cat %v method %v rule %v, want manual %d", id, row.CategoryID, row.CategorizationMethod, row.CategorizationRuleID, catID)
			}
			if !row.Amount.Equal(decimal.NewFromInt(-10)) {
				t.Errorf("row %d amount changed to %s", id, row.Amount)
			}
		}
		if load(ruled.ID).ReviewedAt == nil {
			t.Error("bulk category cleared review state")
		}
		if row := load(adj.ID); row.CategoryID != nil {
			t.Error("non-flow row was categorized")
		}
		if row := load(split.ID); row.CategoryID != nil {
			t.Error("split parent was categorized")
		}

		// A manual edit after the bulk edit must survive the undo.
		if _, err := svc.Update(ctx, userID, bare.ID, service.UpdateTransactionInput{CategoryID: new(other.ID)}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		n, err := svc.BulkUndo(ctx, userID, res.Undo)
		if err != nil || n != 2 {
			t.Fatalf("BulkUndo = %d, %v; want 2 restored", n, err)
		}
		if row := load(ruled.ID); row.CategoryID == nil || *row.CategoryID != other.ID ||
			*row.CategorizationMethod != "rule" || row.CategorizationRuleID == nil || *row.CategorizationRuleID != rule.ID {
			t.Errorf("rule row not restored: %+v", row)
		}
		if row := load(plaid.ID); *row.CategorizationMethod != "plaid_default" || *row.CategoryID != other.ID {
			t.Errorf("plaid row not restored: %+v", row)
		}
		if row := load(bare.ID); row.CategoryID == nil || *row.CategoryID != other.ID {
			t.Errorf("row edited after the bulk edit was overwritten by undo: %+v", row)
		}
		if n, err := svc.BulkUndo(ctx, userID, res.Undo); err != nil || n != 0 {
			t.Errorf("second undo = %d, %v; want 0 (already restored)", n, err)
		}
	})

	t.Run("clear category", func(t *testing.T) {
		row := mk(withManualCategory(catID))
		res, err := svc.BulkEdit(ctx, userID, []int64{row.ID}, service.BulkEditInput{ClearCategory: true})
		if err != nil || res.Updated != 1 {
			t.Fatalf("BulkEdit = %+v, %v", res, err)
		}
		if got := load(row.ID); got.CategoryID != nil || got.CategorizationMethod != nil {
			t.Errorf("category not cleared: %+v", got)
		}
	})

	t.Run("hide, unhide and undo leave the ledger alone", func(t *testing.T) {
		a, b := mk(), mk()
		res, err := svc.BulkEdit(ctx, userID, []int64{a.ID, b.ID}, service.BulkEditInput{Hidden: new(true)})
		if err != nil || res.Updated != 2 {
			t.Fatalf("hide = %+v, %v", res, err)
		}
		if load(a.ID).HiddenAt == nil || load(b.ID).HiddenAt == nil {
			t.Fatal("rows not hidden")
		}
		// Hiding again changes nothing.
		if again, err := svc.BulkEdit(ctx, userID, []int64{a.ID}, service.BulkEditInput{Hidden: new(true)}); err != nil || again.Updated != 0 {
			t.Errorf("re-hide = %+v, %v; want 0 updated", again, err)
		}
		if n, err := svc.BulkUndo(ctx, userID, res.Undo); err != nil || n != 2 {
			t.Fatalf("undo hide = %d, %v", n, err)
		}
		if load(a.ID).HiddenAt != nil {
			t.Error("undo did not unhide")
		}
		if _, err := svc.BulkEdit(ctx, userID, []int64{a.ID}, service.BulkEditInput{Hidden: new(true)}); err != nil {
			t.Fatal(err)
		}
		un, err := svc.BulkEdit(ctx, userID, []int64{a.ID, b.ID}, service.BulkEditInput{Hidden: new(false)})
		if err != nil || un.Updated != 1 || load(a.ID).HiddenAt != nil {
			t.Errorf("unhide = %+v, %v", un, err)
		}
		if !load(a.ID).Amount.Equal(decimal.NewFromInt(-10)) {
			t.Error("hide changed the amount")
		}
	})

	t.Run("another user's rows and rules are untouchable", func(t *testing.T) {
		otherUser := seedTestUser(t, g)
		acc := &model.Account{UserID: otherUser, Name: "bulk-other", InstitutionSlug: "fixture", AccountType: "checking"}
		if err := g.Create(acc).Error; err != nil {
			t.Fatalf("seed account: %v", err)
		}
		t.Cleanup(func() {
			g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
			g.Unscoped().Delete(&model.Account{}, acc.ID)
		})
		theirs := mkTxn(t, g, otherUser, acc.ID)
		theirRule := &model.CategorizationRule{UserID: otherUser, Pattern: "X", MatchType: "contains", CategoryID: new(catID), Priority: 10, IsActive: true}
		if err := g.Create(theirRule).Error; err != nil {
			t.Fatalf("seed rule: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, theirRule.ID) })

		res, err := svc.BulkEdit(ctx, userID, []int64{theirs.ID}, service.BulkEditInput{CategoryID: new(catID)})
		if err != nil || res.Updated != 0 || load(theirs.ID).CategoryID != nil {
			t.Fatalf("cross-user edit = %+v, %v", res, err)
		}
		if n, err := svc.BulkUndo(ctx, userID, []repository.BulkEditPrior{{ID: theirs.ID, CategoryID: new(catID), UpdatedAt: load(theirs.ID).UpdatedAt}}); err != nil || n != 0 {
			t.Fatalf("cross-user undo = %d, %v", n, err)
		}

		// Undo can't attach someone else's rule id to my row.
		mine := mk()
		edit, err := svc.BulkEdit(ctx, userID, []int64{mine.ID}, service.BulkEditInput{Hidden: new(true)})
		if err != nil || len(edit.Undo) != 1 {
			t.Fatalf("BulkEdit: %+v, %v", edit, err)
		}
		forged := edit.Undo[0]
		m := "rule"
		forged.CategoryID, forged.CategorizationMethod, forged.CategorizationRuleID = new(catID), &m, new(theirRule.ID)
		if n, err := svc.BulkUndo(ctx, userID, []repository.BulkEditPrior{forged}); err != nil || n != 1 {
			t.Fatalf("undo = %d, %v", n, err)
		}
		if got := load(mine.ID); got.CategorizationRuleID != nil {
			t.Errorf("undo attached another user's rule %d", *got.CategorizationRuleID)
		}
	})

	t.Run("validation", func(t *testing.T) {
		row := mk()
		tooMany := make([]int64, service.MaxReviewBatch+1)
		cases := []struct {
			name string
			ids  []int64
			in   service.BulkEditInput
			want error
		}{
			{"no action", []int64{row.ID}, service.BulkEditInput{}, service.ErrInvalidBulkEdit},
			{"two actions", []int64{row.ID}, service.BulkEditInput{CategoryID: new(catID), Hidden: new(true)}, service.ErrInvalidBulkEdit},
			{"no ids", nil, service.BulkEditInput{Hidden: new(true)}, service.ErrInvalidReviewBatch},
			{"too many ids", tooMany, service.BulkEditInput{Hidden: new(true)}, service.ErrInvalidReviewBatch},
			{"unknown category", []int64{row.ID}, service.BulkEditInput{CategoryID: new(int64(1 << 40))}, service.ErrInvalidCategory},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := svc.BulkEdit(ctx, userID, tc.ids, tc.in); !errors.Is(err, tc.want) {
					t.Errorf("err = %v, want %v", err, tc.want)
				}
			})
		}
		bogus := "made_up"
		if _, err := svc.BulkUndo(ctx, userID, []repository.BulkEditPrior{{ID: row.ID, CategorizationMethod: &bogus}}); !errors.Is(err, service.ErrInvalidBulkEdit) {
			t.Errorf("bogus method err = %v", err)
		}
	})
}
