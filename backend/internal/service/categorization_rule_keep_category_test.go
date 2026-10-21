package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestCategorizationRuleService_KeepCategoryRule (#553): a rule without a
// category is valid when it takes another action, and preview, Apply and
// first-match-wins all leave each row's category alone.
func TestCategorizationRuleService_KeepCategoryRule(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()
	otherCat := firstSystemCategory(t, g)

	t.Run("validation", func(t *testing.T) {
		cases := []struct {
			name    string
			in      service.CreateRuleInput
			wantErr error
		}{
			{"no category and no action", service.CreateRuleInput{Pattern: "UBER", MatchType: "contains"}, service.ErrRuleHasNoAction},
			{"blank rename is no action", service.CreateRuleInput{Pattern: "UBER", MatchType: "contains", RenameMerchant: new("  ")}, service.ErrRuleHasNoAction},
			{"unknown category still rejected", service.CreateRuleInput{Pattern: "UBER", MatchType: "contains", CategoryID: new(int64(9_999_999))}, service.ErrUnknownCategory},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := svc.Create(ctx, userID, tc.in); !errors.Is(err, tc.wantErr) {
					t.Errorf("Create err = %v, want %v", err, tc.wantErr)
				}
				if _, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{Pattern: tc.in.Pattern, MatchType: tc.in.MatchType, CategoryID: tc.in.CategoryID, RenameMerchant: tc.in.RenameMerchant, IsActive: true}); !errors.Is(err, tc.wantErr) {
					t.Errorf("PreviewDraft err = %v, want %v", err, tc.wantErr)
				}
			})
		}
	})

	uncat := mkTxn(t, g, userID, accountID, withDescription("UBER TRIP MAY"))
	plaidDef := mkTxn(t, g, userID, accountID, withDescription("UBER TRIP JUN"), withPlaidDefaultCategory(otherCat))
	manual := mkTxn(t, g, userID, accountID, withDescription("UBER manual"), withManualCategory(fixtureCat))
	other := mkTxn(t, g, userID, accountID, withDescription("SHELL GAS"))

	// A categorizing rule for the same merchant, placed below the tag-only
	// rule: first-match-wins means it never runs on UBER rows.
	below, err := svc.Create(ctx, userID, service.CreateRuleInput{Pattern: "UBER", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 10})
	if err != nil {
		t.Fatalf("Create categorizing rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, below.ID) })

	draft, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{
		Pattern: "UBER", MatchType: "contains", Priority: 50, IsActive: true, AddTags: []string{"reimbursable"},
	})
	if err != nil {
		t.Fatalf("PreviewDraft: %v", err)
	}
	if draft.Matched != 2 || draft.WouldTag != 2 || draft.WouldRecategorize != 0 || draft.WouldChange != 2 || draft.SkippedManual != 1 {
		t.Errorf("draft = %+v, want matched 2, would_tag 2, would_recategorize 0, would_change 2, skipped_manual 1", draft)
	}

	rule, err := svc.Create(ctx, userID, service.CreateRuleInput{
		Pattern: "UBER", MatchType: "contains", Priority: 50, AddTags: []string{"reimbursable"},
	})
	if err != nil {
		t.Fatalf("Create tag-only rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })
	if rule.CategoryID != nil {
		t.Fatalf("stored category_id = %d, want NULL", *rule.CategoryID)
	}

	res, err := svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Updated != 2 || res.Tagged != 2 || res.Recategorized != 0 || res.SkippedManual != 1 {
		t.Errorf("Apply = %+v, want updated 2, tagged 2, recategorized 0, skipped_manual 1", res)
	}

	load := func(id int64) model.Transaction {
		var got model.Transaction
		if err := g.First(&got, id).Error; err != nil {
			t.Fatalf("load %d: %v", id, err)
		}
		return got
	}
	if got := load(uncat.ID); got.CategoryID != nil || got.CategorizationMethod != nil || got.CategorizationRuleID != nil {
		t.Errorf("uncategorized row: category %v method %v rule %v, want all nil", got.CategoryID, got.CategorizationMethod, got.CategorizationRuleID)
	} else if strings.Join(got.Tags, ",") != "reimbursable" {
		t.Errorf("uncategorized row tags = %v, want [reimbursable]", got.Tags)
	}
	if got := load(plaidDef.ID); got.CategoryID == nil || *got.CategoryID != otherCat ||
		got.CategorizationMethod == nil || *got.CategorizationMethod != "plaid_default" || got.CategorizationRuleID != nil {
		t.Errorf("plaid_default row: category %v method %v rule %v, want %d/plaid_default/nil", got.CategoryID, got.CategorizationMethod, got.CategorizationRuleID, otherCat)
	} else if strings.Join(got.Tags, ",") != "reimbursable" {
		t.Errorf("plaid_default row tags = %v, want [reimbursable]", got.Tags)
	}
	if got := load(manual.ID); len(got.Tags) != 0 || *got.CategoryID != fixtureCat {
		t.Errorf("manual row changed: tags %v category %v", got.Tags, got.CategoryID)
	}
	if got := load(other.ID); got.CategoryID != nil || len(got.Tags) != 0 {
		t.Errorf("non-matching row changed: tags %v category %v", got.Tags, got.CategoryID)
	}

	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("re-Apply updated = %d, want 0 (idempotent)", res.Updated)
	}

	t.Run("clear_category_id turns a categorizing rule into keep-category", func(t *testing.T) {
		got, err := svc.Update(ctx, userID, below.ID, service.UpdateRuleInput{ClearCategoryID: true})
		if !errors.Is(err, service.ErrRuleHasNoAction) {
			t.Fatalf("clear category on a rule with no other action: err = %v (rule %+v), want ErrRuleHasNoAction", err, got)
		}
		reviewed := true
		got, err = svc.Update(ctx, userID, below.ID, service.UpdateRuleInput{ClearCategoryID: true, MarkReviewed: &reviewed})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.CategoryID != nil {
			t.Errorf("category_id after clear = %d, want NULL", *got.CategoryID)
		}
		got, err = svc.Update(ctx, userID, below.ID, service.UpdateRuleInput{CategoryID: new(fixtureCat)})
		if err != nil || got.CategoryID == nil || *got.CategoryID != fixtureCat {
			t.Errorf("set category again: rule %+v err %v, want category %d", got, err, fixtureCat)
		}
	})

	t.Run("another user's rows are untouched", func(t *testing.T) {
		otherUser := seedTestUser(t, g)
		if res, err := svc.Apply(ctx, otherUser, ""); err != nil || res.Updated != 0 {
			t.Errorf("other user's Apply = %+v, %v; want nothing updated", res, err)
		}
		if _, err := svc.Get(ctx, otherUser, rule.ID); !errors.Is(err, service.ErrRuleNotFound) {
			t.Errorf("other user Get err = %v, want ErrRuleNotFound", err)
		}
	})
}
