package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

func decPtr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

func withAmount(s string) func(*model.Transaction) {
	return func(tx *model.Transaction) { tx.Amount = decimal.RequireFromString(s) }
}

// TestCategorizationRuleService_AccountAndAmountConditions covers #550 end to
// end: create validation, the draft preview and bulk Apply agree, an
// account-bound rule leaves another account's rows alone, and amount bands
// route one merchant to two categories.
func TestCategorizationRuleService_AccountAndAmountConditions(t *testing.T) {
	svc, userID, joint, smallCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	other := &model.Account{UserID: userID, Name: "rule-cond-other-" + suffix, InstitutionSlug: "fixture", AccountType: "checking"}
	if err := g.Create(other).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	bigCat := &model.Category{Name: "RuleCondBig", Slug: "rule-cond-big-" + suffix}
	if err := g.Create(bigCat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	// LIFO: rules and rows go before the accounts/categories they reference.
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", other.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, other.ID)
		g.Unscoped().Delete(&model.Category{}, bigCat.ID)
	})
	t.Cleanup(func() { g.Unscoped().Where("user_id = ?", userID).Delete(&model.CategorizationRule{}) })

	t.Run("validation", func(t *testing.T) {
		stranger := seedTestUser(t, g)
		foreign := &model.Account{UserID: stranger, Name: "rule-cond-foreign-" + suffix, InstitutionSlug: "fixture", AccountType: "checking"}
		if err := g.Create(foreign).Error; err != nil {
			t.Fatalf("seed foreign account: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, foreign.ID) })

		cases := []struct {
			name string
			in   service.CreateRuleInput
			want error
		}{
			{"another user's account", service.CreateRuleInput{AccountID: &foreign.ID}, service.ErrUnknownRuleAccount},
			{"negative min", service.CreateRuleInput{AmountMin: decPtr("-1")}, service.ErrInvalidAmountRange},
			{"min above max", service.CreateRuleInput{AmountMin: decPtr("20"), AmountMax: decPtr("10")}, service.ErrInvalidAmountRange},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				in := tc.in
				in.Pattern, in.MatchType, in.CategoryID = "X", "contains", smallCat
				if _, err := svc.Create(ctx, userID, in); !errors.Is(err, tc.want) {
					t.Errorf("Create err = %v, want %v", err, tc.want)
				}
			})
		}
	})

	small := mkTxn(t, g, userID, joint, withDescription("AMZN Mktp US"), withAmount("-12.99"))
	big := mkTxn(t, g, userID, joint, withDescription("AMZN Mktp US"), withAmount("-649.00"))
	mid := mkTxn(t, g, userID, joint, withDescription("AMZN Mktp US"), withAmount("-89.50"))
	elsewhere := mkTxn(t, g, userID, other.ID, withDescription("AMZN Mktp US"), withAmount("-12.99"))

	smallRule := service.CreateRuleInput{Pattern: "AMZN", MatchType: "contains", CategoryID: smallCat, AccountID: &joint, AmountMax: decPtr("20"), Priority: 20}
	preview, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{
		Pattern: smallRule.Pattern, MatchType: smallRule.MatchType, CategoryID: smallRule.CategoryID,
		AccountID: smallRule.AccountID, AmountMax: smallRule.AmountMax, Priority: smallRule.Priority, IsActive: true,
	})
	if err != nil {
		t.Fatalf("PreviewDraft: %v", err)
	}
	if preview.Matched != 1 || preview.WouldChange != 1 {
		t.Errorf("preview matched/would_change = %d/%d, want 1/1", preview.Matched, preview.WouldChange)
	}
	if _, err := svc.Create(ctx, userID, smallRule); err != nil {
		t.Fatalf("create small rule: %v", err)
	}
	created, err := svc.Create(ctx, userID, service.CreateRuleInput{Pattern: "AMZN", MatchType: "contains", CategoryID: bigCat.ID, AmountMin: decPtr("500"), Priority: 10})
	if err != nil {
		t.Fatalf("create big rule: %v", err)
	}

	res, err := svc.Apply(ctx, userID, "all")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Updated != 2 {
		t.Errorf("Apply updated %d, want 2", res.Updated)
	}

	want := map[int64]*int64{small.ID: &smallCat, big.ID: &bigCat.ID, mid.ID: nil, elsewhere.ID: nil}
	for id, cat := range want {
		var got model.Transaction
		if err := g.First(&got, id).Error; err != nil {
			t.Fatalf("reload %d: %v", id, err)
		}
		switch {
		case cat == nil && got.CategoryID != nil:
			t.Errorf("row %d categorized as %d, want untouched", id, *got.CategoryID)
		case cat != nil && (got.CategoryID == nil || *got.CategoryID != *cat):
			t.Errorf("row %d category = %v, want %d", id, got.CategoryID, *cat)
		}
	}

	// Dropping the amount floor via Update widens the rule to the $89.50 row.
	if _, err := svc.Update(ctx, userID, created.ID, service.UpdateRuleInput{ClearAmountMin: true}); err != nil {
		t.Fatalf("Update clear min: %v", err)
	}
	if res, err = svc.Apply(ctx, userID, "all"); err != nil {
		t.Fatalf("Apply after clear: %v", err)
	}
	// mid and elsewhere now match the unconstrained big rule.
	if res.Updated != 2 {
		t.Errorf("Apply after clear updated %d, want 2", res.Updated)
	}
}
