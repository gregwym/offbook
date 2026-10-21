package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestCategorizationRuleService_UndoApply covers #565: a committed bulk
// apply can be reverted to exactly the prior state of every row it changed.
func TestCategorizationRuleService_UndoApply(t *testing.T) {
	type fixture struct {
		svc                          *service.CategorizationRuleService
		userID, accountID, targetCat int64
		priorCat                     int64
		g                            *gorm.DB
	}
	setup := func(t *testing.T) (fixture, func(...func(*model.Transaction)) *model.Transaction, func(id int64) model.Transaction) {
		svc, userID, accountID, targetCat, g := newRuleApplySvc(t)
		t.Cleanup(func() {
			g.Exec("DELETE FROM rule_apply_runs WHERE user_id = ?", userID)
		})
		prior := &model.Category{Name: "UndoPrior", Slug: "undo-prior-" + time.Now().Format("150405.000000")}
		if err := g.Create(prior).Error; err != nil {
			t.Fatalf("seed category: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, prior.ID) })
		rule := &model.CategorizationRule{
			UserID: userID, Pattern: "UNDOME", MatchType: "contains",
			CategoryID: new(targetCat), RenameMerchant: new("Clean Name"),
			HideMatches: true, MarkReviewed: true, AddTags: []string{"bulk"},
			Priority: 10, IsActive: true,
		}
		if err := g.Create(rule).Error; err != nil {
			t.Fatalf("seed rule: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })
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
		return fixture{svc: svc, userID: userID, accountID: accountID, targetCat: targetCat, priorCat: prior.ID, g: g}, mk, load
	}

	t.Run("restores every action and leaves the amount alone", func(t *testing.T) {
		f, mk, load := setup(t)
		ctx := context.Background()
		row := mk(withDescription("UNDOME coffee"), withPlaidDefaultCategory(f.priorCat), func(tx *model.Transaction) {
			tx.MerchantName = new("RAW MERCHANT")
			tx.Tags = model.Tags{"keep"}
		})
		untouched := mk(withDescription("OTHER row"))
		before := load(row.ID)

		res, err := f.svc.Apply(ctx, f.userID, "")
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if res.Updated != 1 || res.RunID == nil {
			t.Fatalf("Apply result = %+v, want 1 updated with a run id", res)
		}
		applied := load(row.ID)
		if applied.HiddenAt == nil || applied.ReviewedAt == nil || *applied.CategoryID != f.targetCat {
			t.Fatalf("apply did not take effect: %+v", applied)
		}

		latest, err := f.svc.LatestApplyRun(ctx, f.userID)
		if err != nil || latest.ID != *res.RunID || latest.RowsChanged != 1 || latest.UndoneAt != nil {
			t.Fatalf("LatestApplyRun = %+v, %v", latest, err)
		}

		run, err := f.svc.UndoApply(ctx, f.userID, *res.RunID)
		if err != nil {
			t.Fatalf("UndoApply: %v", err)
		}
		if run.UndoneAt == nil || *run.RowsRestored != 1 || *run.RowsKept != 0 {
			t.Fatalf("undo run = %+v", run)
		}
		after := load(row.ID)
		if after.CategoryID == nil || *after.CategoryID != f.priorCat {
			t.Errorf("category = %v, want %d", after.CategoryID, f.priorCat)
		}
		if after.CategorizationMethod == nil || *after.CategorizationMethod != "plaid_default" {
			t.Errorf("method = %v, want plaid_default", after.CategorizationMethod)
		}
		if after.CategorizationRuleID != nil {
			t.Errorf("rule id = %v, want nil", *after.CategorizationRuleID)
		}
		if after.MerchantName == nil || *after.MerchantName != "RAW MERCHANT" {
			t.Errorf("merchant = %v, want RAW MERCHANT", after.MerchantName)
		}
		if after.HiddenAt != nil || after.ReviewedAt != nil {
			t.Errorf("hidden/reviewed = %v/%v, want nil/nil", after.HiddenAt, after.ReviewedAt)
		}
		if len(after.Tags) != 1 || after.Tags[0] != "keep" {
			t.Errorf("tags = %v, want [keep]", after.Tags)
		}
		if !after.Amount.Equal(before.Amount) {
			t.Errorf("amount = %s, want %s", after.Amount, before.Amount)
		}
		if got := load(untouched.ID); got.CategoryID != nil || got.HiddenAt != nil {
			t.Errorf("unmatched row changed: %+v", got)
		}
	})

	t.Run("keeps a row edited after the run", func(t *testing.T) {
		f, mk, load := setup(t)
		ctx := context.Background()
		edited := mk(withDescription("UNDOME edited later"))
		plain := mk(withDescription("UNDOME plain"))

		res, err := f.svc.Apply(ctx, f.userID, "")
		if err != nil || res.Updated != 2 {
			t.Fatalf("Apply = %+v, %v", res, err)
		}
		// The user re-categorizes one row by hand after the run.
		row := load(edited.ID)
		row.CategoryID = new(f.priorCat)
		row.CategorizationMethod = new("manual")
		if err := f.g.Save(&row).Error; err != nil {
			t.Fatalf("manual edit: %v", err)
		}

		run, err := f.svc.UndoApply(ctx, f.userID, *res.RunID)
		if err != nil {
			t.Fatalf("UndoApply: %v", err)
		}
		if *run.RowsRestored != 1 || *run.RowsKept != 1 {
			t.Fatalf("restored/kept = %d/%d, want 1/1", *run.RowsRestored, *run.RowsKept)
		}
		if got := load(edited.ID); got.CategoryID == nil || *got.CategoryID != f.priorCat || got.HiddenAt == nil {
			t.Errorf("edited row was overwritten: %+v", got)
		}
		if got := load(plain.ID); got.CategoryID != nil || got.HiddenAt != nil {
			t.Errorf("plain row not restored: %+v", got)
		}
	})

	t.Run("second undo and older run are not undoable", func(t *testing.T) {
		f, mk, _ := setup(t)
		ctx := context.Background()
		mk(withDescription("UNDOME first"))
		first, err := f.svc.Apply(ctx, f.userID, "")
		if err != nil || first.RunID == nil {
			t.Fatalf("Apply 1 = %+v, %v", first, err)
		}
		mk(withDescription("UNDOME second"))
		second, err := f.svc.Apply(ctx, f.userID, "")
		if err != nil || second.RunID == nil || second.Updated != 1 {
			t.Fatalf("Apply 2 = %+v, %v", second, err)
		}
		if _, err := f.svc.UndoApply(ctx, f.userID, *first.RunID); !errors.Is(err, service.ErrRuleApplyNotUndoable) {
			t.Errorf("undo older run err = %v, want ErrRuleApplyNotUndoable", err)
		}
		if _, err := f.svc.UndoApply(ctx, f.userID, *second.RunID); err != nil {
			t.Fatalf("undo latest: %v", err)
		}
		if _, err := f.svc.UndoApply(ctx, f.userID, *second.RunID); !errors.Is(err, service.ErrRuleApplyNotUndoable) {
			t.Errorf("second undo err = %v, want ErrRuleApplyNotUndoable", err)
		}
	})

	t.Run("dry run and no-op runs record nothing", func(t *testing.T) {
		f, mk, _ := setup(t)
		ctx := context.Background()
		mk(withDescription("UNDOME dry"))
		preview, err := f.svc.Preview(ctx, f.userID, "")
		if err != nil || preview.RunID != nil {
			t.Fatalf("Preview = %+v, %v; want no run id", preview, err)
		}
		if _, err := f.svc.LatestApplyRun(ctx, f.userID); !errors.Is(err, service.ErrRuleApplyRunNotFound) {
			t.Fatalf("LatestApplyRun after preview err = %v, want not found", err)
		}
		if _, err := f.svc.Apply(ctx, f.userID, ""); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		again, err := f.svc.Apply(ctx, f.userID, "")
		if err != nil || again.Updated != 0 || again.RunID != nil {
			t.Fatalf("idempotent re-apply = %+v, %v; want 0 updated, no run", again, err)
		}
	})

	t.Run("another user's run is not found", func(t *testing.T) {
		f, mk, _ := setup(t)
		other, _, _ := setup(t)
		ctx := context.Background()
		mk(withDescription("UNDOME mine"))
		res, err := f.svc.Apply(ctx, f.userID, "")
		if err != nil || res.RunID == nil {
			t.Fatalf("Apply = %+v, %v", res, err)
		}
		if _, err := f.svc.UndoApply(ctx, other.userID, *res.RunID); !errors.Is(err, service.ErrRuleApplyRunNotFound) {
			t.Errorf("cross-user undo err = %v, want ErrRuleApplyRunNotFound", err)
		}
		if _, err := f.svc.LatestApplyRun(ctx, other.userID); !errors.Is(err, service.ErrRuleApplyRunNotFound) {
			t.Errorf("cross-user latest err = %v, want not found", err)
		}
	})
}
