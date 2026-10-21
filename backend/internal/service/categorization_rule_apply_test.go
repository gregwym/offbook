package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// newRuleApplySvc builds a CategorizationRuleService with bulk-apply
// wiring, plus a seeded user + account + spare categories for tests.
func newRuleApplySvc(t *testing.T) (svc *service.CategorizationRuleService, userID, accountID, fixtureCat int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)

	ctx := context.Background()
	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID:          userID,
		Name:            "rule-apply-" + suffix,
		InstitutionSlug: "fixture",
		AccountType:     "checking",
	}
	if err := g.WithContext(ctx).Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})

	cat := &model.Category{
		Name:     "RuleApplyFixture",
		Slug:     "rule-apply-fixture-" + suffix,
		IsSystem: false,
	}
	if err := g.WithContext(ctx).Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	svc = service.NewCategorizationRuleService(
		repository.NewCategorizationRuleRepository(g),
		repository.NewCategoryRepository(g),
	).WithBulkApply(repository.NewTransactionRepository(g), g).
		WithAccounts(repository.NewAccountRepository(g))

	return svc, userID, acc.ID, cat.ID, g
}

// mkTxn inserts a transaction directly via gorm (bypassing transaction_service
// to keep the fixture deterministic — we own the categorization fields).
func mkTxn(t *testing.T, g *gorm.DB, userID, accountID int64, opts ...func(*model.Transaction)) *model.Transaction {
	t.Helper()
	tx := &model.Transaction{
		UserID:          userID,
		AccountID:       accountID,
		Amount:          decimal.NewFromInt(-10),
		TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	}
	for _, opt := range opts {
		opt(tx)
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed txn: %v", err)
	}
	return tx
}

func withDescription(s string) func(*model.Transaction) {
	return func(tx *model.Transaction) {
		v := s
		tx.Description = &v
	}
}

func withManualCategory(catID int64) func(*model.Transaction) {
	return func(tx *model.Transaction) {
		id := catID
		method := "manual"
		tx.CategoryID = &id
		tx.CategorizationMethod = &method
	}
}

func withPlaidDefaultCategory(catID int64) func(*model.Transaction) {
	return func(tx *model.Transaction) {
		id := catID
		method := "plaid_default"
		tx.CategoryID = &id
		tx.CategorizationMethod = &method
	}
}

// TestCategorizationRuleService_Apply_ScopeAll: 100 fixture txns, 1 rule
// that matches half of them — those get updated; manual rows are untouched.
func TestCategorizationRuleService_Apply_ScopeAll(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	// Seed one rule.
	rule := &model.CategorizationRule{
		UserID: userID, Pattern: "WHOLEFDS", MatchType: "contains",
		CategoryID: new(fixtureCat), Priority: 50, IsActive: true,
	}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	// 50 matching, 30 non-matching, 20 manual (which should be skipped even
	// though they contain WHOLEFDS — verifies the manual-wins rule).
	for i := 0; i < 50; i++ {
		mkTxn(t, g, userID, accountID, withDescription("WHOLEFDS #"+fmt.Sprint(i)))
	}
	for i := 0; i < 30; i++ {
		mkTxn(t, g, userID, accountID, withDescription("OTHER #"+fmt.Sprint(i)))
	}
	for i := 0; i < 20; i++ {
		mkTxn(t, g, userID, accountID,
			withDescription("WHOLEFDS manual #"+fmt.Sprint(i)),
			withManualCategory(fixtureCat))
	}

	result, err := svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Scanned != 100 {
		t.Errorf("Scanned = %d, want 100", result.Scanned)
	}
	if result.Updated != 50 {
		t.Errorf("Updated = %d, want 50", result.Updated)
	}
	if result.SkippedManual != 20 {
		t.Errorf("SkippedManual = %d, want 20", result.SkippedManual)
	}

	// Verify the manual rows still have categorization_method=manual.
	var manualCount int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND categorization_method = ?", userID, "manual").
		Count(&manualCount).Error; err != nil {
		t.Fatalf("count manual: %v", err)
	}
	if manualCount != 20 {
		t.Errorf("manual rows post-apply = %d, want 20", manualCount)
	}

	// Verify the matched rows are now method=rule with rule_id set.
	var ruleCount int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND categorization_method = ? AND categorization_rule_id = ?",
			userID, "rule", rule.ID).
		Count(&ruleCount).Error; err != nil {
		t.Fatalf("count rule rows: %v", err)
	}
	if ruleCount != 50 {
		t.Errorf("rule-categorized rows = %d, want 50", ruleCount)
	}

	// Second run is a no-op for already-rule-set rows.
	result2, err := svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("Apply (rerun): %v", err)
	}
	if result2.Updated != 0 {
		t.Errorf("rerun Updated = %d, want 0 (idempotent)", result2.Updated)
	}
}

// TestCategorizationRuleService_Apply_ScopeUncategorized: only rows with
// NULL category_id are scanned. Pre-categorized rows (manual or
// plaid_default) are not in the scan set at all.
func TestCategorizationRuleService_Apply_ScopeUncategorized(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	// Second category so we can prove pre-categorized rows weren't touched.
	other := &model.Category{Name: "Other", Slug: "uncat-other-" + time.Now().Format("150405.000000")}
	if err := g.Create(other).Error; err != nil {
		t.Fatalf("seed other: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, other.ID) })

	rule := &model.CategorizationRule{
		UserID: userID, Pattern: "WHOLEFDS", MatchType: "contains",
		CategoryID: new(fixtureCat), Priority: 50, IsActive: true,
	}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	// 3 uncategorized matching, 2 plaid_default already-categorized matching.
	for i := 0; i < 3; i++ {
		mkTxn(t, g, userID, accountID, withDescription("WHOLEFDS #"+fmt.Sprint(i)))
	}
	for i := 0; i < 2; i++ {
		mkTxn(t, g, userID, accountID,
			withDescription("WHOLEFDS plaid #"+fmt.Sprint(i)),
			withPlaidDefaultCategory(other.ID))
	}

	result, err := svc.Apply(ctx, userID, "uncategorized")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Scanned != 3 {
		t.Errorf("Scanned = %d, want 3", result.Scanned)
	}
	if result.Updated != 3 {
		t.Errorf("Updated = %d, want 3", result.Updated)
	}
	if result.SkippedManual != 0 {
		t.Errorf("SkippedManual = %d, want 0", result.SkippedManual)
	}

	// plaid_default rows remain pointed at `other`.
	var stillPlaid int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND categorization_method = ? AND category_id = ?",
			userID, "plaid_default", other.ID).
		Count(&stillPlaid).Error; err != nil {
		t.Fatalf("count plaid_default: %v", err)
	}
	if stillPlaid != 2 {
		t.Errorf("plaid_default rows post-apply = %d, want 2 (uncategorized scope must not touch them)", stillPlaid)
	}
}

// TestCategorizationRuleService_Apply_TenantIsolation: a user's Apply
// must not touch another user's transactions.
func TestCategorizationRuleService_Apply_TenantIsolation(t *testing.T) {
	svcA, userA, accountA, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	// User B has a transaction that user A's rule would otherwise match.
	userB := seedTestUser(t, g)
	suffix := time.Now().Format("150405.000000")
	accB := &model.Account{
		UserID: userB, Name: "B-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed account B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", accB.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, accB.ID)
	})

	ruleA := &model.CategorizationRule{
		UserID: userA, Pattern: "WHOLEFDS", MatchType: "contains",
		CategoryID: new(fixtureCat), Priority: 50, IsActive: true,
	}
	if err := g.Create(ruleA).Error; err != nil {
		t.Fatalf("seed rule A: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, ruleA.ID) })

	mkTxn(t, g, userA, accountA, withDescription("WHOLEFDS A1"))
	bTxn := mkTxn(t, g, userB, accB.ID, withDescription("WHOLEFDS B1"))

	result, err := svcA.Apply(ctx, userA, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Scanned != 1 || result.Updated != 1 {
		t.Errorf("user A scope: scanned=%d updated=%d, want 1/1 (user B's row must not be in scope)",
			result.Scanned, result.Updated)
	}

	// User B's row is unchanged.
	var afterB model.Transaction
	if err := g.First(&afterB, bTxn.ID).Error; err != nil {
		t.Fatalf("fetch B txn: %v", err)
	}
	if afterB.CategoryID != nil {
		t.Errorf("user B's txn category_id = %v, want nil — cross-tenant write", afterB.CategoryID)
	}
}

// TestCategorizationRuleService_Apply_BadScopeRejected catches typo/scope-
// validation bugs without a Postgres round-trip per case.
func TestCategorizationRuleService_Apply_BadScopeRejected(t *testing.T) {
	svc, userID, _, _, _ := newRuleApplySvc(t)
	ctx := context.Background()
	if _, err := svc.Apply(ctx, userID, "garbage"); !errors.Is(err, service.ErrInvalidApplyScope) {
		t.Errorf("bad scope err = %v, want ErrInvalidApplyScope", err)
	}
}

// TestCategorizationRuleService_Preview_WritesNothing: Preview reports the
// rows Apply would change (and skips manual picks) without touching them.
func TestCategorizationRuleService_Preview_WritesNothing(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	rule := &model.CategorizationRule{
		UserID: userID, Pattern: "WHOLEFDS", MatchType: "contains",
		CategoryID: new(fixtureCat), Priority: 50, IsActive: true,
	}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	for i := 0; i < 3; i++ {
		mkTxn(t, g, userID, accountID, withDescription("WHOLEFDS #"+fmt.Sprint(i)))
	}
	mkTxn(t, g, userID, accountID, withDescription("WHOLEFDS manual"), withManualCategory(fixtureCat))

	preview, err := svc.Preview(ctx, userID, "all")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !preview.DryRun || preview.Updated != 3 || preview.SkippedManual != 1 {
		t.Errorf("preview = %+v, want dry_run, updated 3, skipped_manual 1", preview)
	}

	var ruled int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND categorization_method = ?", userID, "rule").
		Count(&ruled).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if ruled != 0 {
		t.Errorf("rule-categorized rows after Preview = %d, want 0", ruled)
	}

	applied, err := svc.Apply(ctx, userID, "all")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied.DryRun || applied.Updated != preview.Updated {
		t.Errorf("apply = %+v, want updated %d (matching preview)", applied, preview.Updated)
	}
}

// TestCategorizationRuleService_PreviewDraft: an unsaved rule's impact respects
// priority against stored rules, skips manual picks, supports edit-replace,
// and writes nothing.
func TestCategorizationRuleService_PreviewDraft(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	rule := &model.CategorizationRule{
		UserID: userID, Pattern: "WHOLEFDS", MatchType: "contains",
		CategoryID: new(fixtureCat), Priority: 50, IsActive: true,
	}
	if err := g.Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	for i := 0; i < 3; i++ {
		mkTxn(t, g, userID, accountID, withDescription("WHOLEFDS #"+fmt.Sprint(i)))
	}
	mkTxn(t, g, userID, accountID, withDescription("WHOLEFDS manual"), withManualCategory(fixtureCat))
	mkTxn(t, g, userID, accountID, withDescription("SHELL GAS"))

	draft := func(mut func(*service.DraftRuleInput)) service.DraftRuleInput {
		in := service.DraftRuleInput{Pattern: "WHOLEFDS", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 90, IsActive: true}
		mut(&in)
		return in
	}

	tests := []struct {
		name        string
		in          service.DraftRuleInput
		wantMatched int
		wantChange  int
	}{
		{"higher priority wins", draft(func(*service.DraftRuleInput) {}), 3, 3},
		{"lower priority loses to stored rule", draft(func(in *service.DraftRuleInput) { in.Priority = 10 }), 0, 0},
		{"inactive draft matches nothing", draft(func(in *service.DraftRuleInput) { in.IsActive = false }), 0, 0},
		{"edit replaces stored version", draft(func(in *service.DraftRuleInput) { in.RuleID = &rule.ID; in.Pattern = "SHELL"; in.Priority = 50 }), 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.PreviewDraft(ctx, userID, tc.in)
			if err != nil {
				t.Fatalf("PreviewDraft: %v", err)
			}
			if got.Matched != tc.wantMatched || got.WouldChange != tc.wantChange || got.SkippedManual != 1 {
				t.Errorf("got %+v, want matched %d, would_change %d, skipped_manual 1", got, tc.wantMatched, tc.wantChange)
			}
			if len(got.Samples) != tc.wantMatched {
				t.Errorf("samples = %v, want %d", got.Samples, tc.wantMatched)
			}
		})
	}

	if _, err := svc.PreviewDraft(ctx, userID, draft(func(in *service.DraftRuleInput) { in.MatchType = "regex"; in.Pattern = "(" })); !errors.Is(err, service.ErrInvalidRegex) {
		t.Errorf("invalid regex err = %v, want ErrInvalidRegex", err)
	}
	other := int64(1 << 40)
	if _, err := svc.PreviewDraft(ctx, userID, draft(func(in *service.DraftRuleInput) { in.RuleID = &other })); !errors.Is(err, service.ErrRuleNotFound) {
		t.Errorf("unknown rule err = %v, want ErrRuleNotFound", err)
	}

	var ruled int64
	if err := g.Model(&model.Transaction{}).
		Where("user_id = ? AND categorization_method = ?", userID, "rule").
		Count(&ruled).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if ruled != 0 {
		t.Errorf("rule-categorized rows after PreviewDraft = %d, want 0", ruled)
	}
}

// TestCategorizationRuleService_Apply_RenameMerchant (#534): a rule with a
// merchant rename writes it on every row it wins, skips manual rows, and a
// re-run is a no-op. Create/Update trim the rename; blank clears it.
func TestCategorizationRuleService_Apply_RenameMerchant(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	rename := "  Amazon "
	rule, err := svc.Create(ctx, userID, service.CreateRuleInput{
		Pattern: "AMZN", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50, RenameMerchant: &rename,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })
	if rule.RenameMerchant == nil || *rule.RenameMerchant != "Amazon" {
		t.Fatalf("stored rename = %v, want Amazon", rule.RenameMerchant)
	}

	a := mkTxn(t, g, userID, accountID, withDescription("AMZN MKTP US*2K4"))
	b := mkTxn(t, g, userID, accountID, withDescription("AMZN Digital*9X1"), func(tx *model.Transaction) {
		v := "AMZN Digital"
		tx.MerchantName = &v
	})
	manual := mkTxn(t, g, userID, accountID, withDescription("AMZN manual"), withManualCategory(fixtureCat))
	other := mkTxn(t, g, userID, accountID, withDescription("SHELL GAS"))

	draft, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{
		RuleID: &rule.ID, Pattern: "AMZN", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50, IsActive: true, RenameMerchant: &rename,
	})
	if err != nil {
		t.Fatalf("PreviewDraft: %v", err)
	}
	if draft.Matched != 2 || draft.WouldChange != 2 || draft.WouldRename != 2 {
		t.Errorf("draft = %+v, want matched 2, would_change 2, would_rename 2", draft)
	}

	res, err := svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Updated != 2 || res.SkippedManual != 1 {
		t.Errorf("Apply = %+v, want updated 2, skipped_manual 1", res)
	}
	merchant := func(id int64) *string {
		var got model.Transaction
		if err := g.First(&got, id).Error; err != nil {
			t.Fatalf("load %d: %v", id, err)
		}
		return got.MerchantName
	}
	for _, id := range []int64{a.ID, b.ID} {
		if m := merchant(id); m == nil || *m != "Amazon" {
			t.Errorf("txn %d merchant = %v, want Amazon", id, m)
		}
	}
	if m := merchant(manual.ID); m != nil {
		t.Errorf("manual row merchant = %q, want untouched (nil)", *m)
	}
	if m := merchant(other.ID); m != nil {
		t.Errorf("non-matching row merchant = %q, want nil", *m)
	}

	res, err = svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("re-Apply: %v", err)
	}
	if res.Updated != 0 {
		t.Errorf("re-Apply updated = %d, want 0 (idempotent)", res.Updated)
	}

	// A changed rename re-applies; a blank one clears the rule's rename and
	// leaves merchants as they are.
	renamed := "Amazon.com"
	if _, err := svc.Update(ctx, userID, rule.ID, service.UpdateRuleInput{RenameMerchant: &renamed}); err != nil {
		t.Fatalf("Update rename: %v", err)
	}
	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 2 {
		t.Errorf("Apply after rename change updated = %d, want 2", res.Updated)
	}
	blank := " "
	updated, err := svc.Update(ctx, userID, rule.ID, service.UpdateRuleInput{RenameMerchant: &blank})
	if err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if updated.RenameMerchant != nil {
		t.Errorf("rename after clear = %q, want nil", *updated.RenameMerchant)
	}
	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("Apply after clear updated = %d, want 0", res.Updated)
	}
	if m := merchant(a.ID); m == nil || *m != "Amazon.com" {
		t.Errorf("merchant after clear = %v, want Amazon.com kept", m)
	}

	long := strings.Repeat("x", 201)
	if _, err := svc.Update(ctx, userID, rule.ID, service.UpdateRuleInput{RenameMerchant: &long}); !errors.Is(err, service.ErrRenameTooLong) {
		t.Errorf("long rename err = %v, want ErrRenameTooLong", err)
	}
}

// TestCategorizationRuleService_Apply_HideMatches (#536): a hiding rule hides
// every non-manual row it wins, the draft preview counts them first, a re-run
// is a no-op, and turning hiding off never unhides.
func TestCategorizationRuleService_Apply_HideMatches(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	a := mkTxn(t, g, userID, accountID, withDescription("CASH SWEEP TO MMF"))
	b := mkTxn(t, g, userID, accountID, withDescription("CASH SWEEP FROM MMF"))
	manual := mkTxn(t, g, userID, accountID, withDescription("CASH SWEEP manual"), withManualCategory(fixtureCat))
	other := mkTxn(t, g, userID, accountID, withDescription("SHELL GAS"))

	draft, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{
		Pattern: "SWEEP", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50, IsActive: true, HideMatches: true,
	})
	if err != nil {
		t.Fatalf("PreviewDraft: %v", err)
	}
	if draft.Matched != 2 || draft.WouldChange != 2 || draft.WouldHide != 2 || draft.SkippedManual != 1 {
		t.Errorf("draft = %+v, want matched 2, would_change 2, would_hide 2, skipped_manual 1", draft)
	}

	rule, err := svc.Create(ctx, userID, service.CreateRuleInput{
		Pattern: "SWEEP", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50, HideMatches: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })
	if !rule.HideMatches {
		t.Fatal("stored hide_matches = false, want true")
	}

	res, err := svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Updated != 2 || res.SkippedManual != 1 {
		t.Errorf("Apply = %+v, want updated 2, skipped_manual 1", res)
	}
	hidden := func(id int64) bool {
		var got model.Transaction
		if err := g.First(&got, id).Error; err != nil {
			t.Fatalf("load %d: %v", id, err)
		}
		return got.HiddenAt != nil
	}
	for _, id := range []int64{a.ID, b.ID} {
		if !hidden(id) {
			t.Errorf("txn %d visible, want hidden", id)
		}
	}
	if hidden(manual.ID) {
		t.Error("manual row hidden, want untouched")
	}
	if hidden(other.ID) {
		t.Error("non-matching row hidden")
	}

	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("re-Apply updated = %d, want 0 (idempotent)", res.Updated)
	}

	// Turning hiding off keeps rows hidden; nothing for Apply to do.
	off := false
	updated, err := svc.Update(ctx, userID, rule.ID, service.UpdateRuleInput{HideMatches: &off})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.HideMatches {
		t.Error("hide_matches after update = true, want false")
	}
	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("Apply after hide off updated = %d, want 0", res.Updated)
	}
	if !hidden(a.ID) {
		t.Error("row unhidden after hide off, want kept hidden")
	}
}

// TestCategorizationRuleService_Apply_MarkReviewed (#538): a review-marking
// rule marks every non-manual row it wins reviewed, the draft preview counts
// them first, a re-run is a no-op, and turning the mark off never un-reviews.
func TestCategorizationRuleService_Apply_MarkReviewed(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	a := mkTxn(t, g, userID, accountID, withDescription("ACME PAYROLL MAY"))
	b := mkTxn(t, g, userID, accountID, withDescription("ACME PAYROLL JUN"))
	manual := mkTxn(t, g, userID, accountID, withDescription("ACME PAYROLL manual"), withManualCategory(fixtureCat))
	other := mkTxn(t, g, userID, accountID, withDescription("SHELL GAS"))

	draft, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{
		Pattern: "PAYROLL", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50, IsActive: true, MarkReviewed: true,
	})
	if err != nil {
		t.Fatalf("PreviewDraft: %v", err)
	}
	if draft.Matched != 2 || draft.WouldChange != 2 || draft.WouldMarkReviewed != 2 || draft.WouldHide != 0 || draft.SkippedManual != 1 {
		t.Errorf("draft = %+v, want matched 2, would_change 2, would_mark_reviewed 2, would_hide 0, skipped_manual 1", draft)
	}

	rule, err := svc.Create(ctx, userID, service.CreateRuleInput{
		Pattern: "PAYROLL", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50, MarkReviewed: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })
	if !rule.MarkReviewed {
		t.Fatal("stored mark_reviewed = false, want true")
	}

	res, err := svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Updated != 2 || res.SkippedManual != 1 {
		t.Errorf("Apply = %+v, want updated 2, skipped_manual 1", res)
	}
	reviewed := func(id int64) bool {
		var got model.Transaction
		if err := g.First(&got, id).Error; err != nil {
			t.Fatalf("load %d: %v", id, err)
		}
		return got.ReviewedAt != nil
	}
	for _, id := range []int64{a.ID, b.ID} {
		if !reviewed(id) {
			t.Errorf("txn %d unreviewed, want reviewed", id)
		}
	}
	if reviewed(other.ID) {
		t.Error("non-matching row marked reviewed")
	}
	if reviewed(manual.ID) {
		t.Error("manual row marked reviewed, want untouched")
	}

	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("re-Apply updated = %d, want 0 (idempotent)", res.Updated)
	}

	// Turning the mark off keeps rows reviewed; nothing for Apply to do.
	off := false
	updated, err := svc.Update(ctx, userID, rule.ID, service.UpdateRuleInput{MarkReviewed: &off})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.MarkReviewed {
		t.Error("mark_reviewed after update = true, want false")
	}
	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("Apply after mark off updated = %d, want 0", res.Updated)
	}
	if !reviewed(a.ID) {
		t.Error("row un-reviewed after mark off, want kept reviewed")
	}
}

// TestCategorizationRuleService_Apply_AddTags (#546): a tagging rule adds its
// tags to every non-manual row it wins, reuses the user's existing spelling,
// the draft preview counts them first, a re-run is a no-op, and clearing the
// rule's tags never untags rows.
func TestCategorizationRuleService_Apply_AddTags(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	a := mkTxn(t, g, userID, accountID, withDescription("UBER TRIP MAY"))
	b := mkTxn(t, g, userID, accountID, withDescription("UBER TRIP JUN"))
	manual := mkTxn(t, g, userID, accountID, withDescription("UBER manual"), withManualCategory(fixtureCat))
	other := mkTxn(t, g, userID, accountID, withDescription("SHELL GAS"))
	// b already carries the tag in the user's own spelling.
	if err := g.Model(&model.Transaction{}).Where("id = ?", b.ID).Update("tags", model.Tags{"Work-Travel"}).Error; err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	draft, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{
		Pattern: "UBER", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50, IsActive: true,
		AddTags: []string{" work-travel ", "Reimbursable", "reimbursable"},
	})
	if err != nil {
		t.Fatalf("PreviewDraft: %v", err)
	}
	if draft.Matched != 2 || draft.WouldTag != 2 || draft.SkippedManual != 1 {
		t.Errorf("draft = %+v, want matched 2, would_tag 2, skipped_manual 1", draft)
	}

	if _, err := svc.Create(ctx, userID, service.CreateRuleInput{
		Pattern: "UBER", MatchType: "contains", CategoryID: new(fixtureCat), AddTags: []string{strings.Repeat("x", service.MaxTagLength+1)},
	}); !errors.Is(err, service.ErrInvalidTags) {
		t.Errorf("Create with an over-long tag: err = %v, want ErrInvalidTags", err)
	}

	rule, err := svc.Create(ctx, userID, service.CreateRuleInput{
		Pattern: "UBER", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50,
		AddTags: []string{" work-travel ", "Reimbursable", "reimbursable"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })
	if got := strings.Join(rule.AddTags, ","); got != "Work-Travel,Reimbursable" {
		t.Fatalf("stored add_tags = %q, want %q (normalized, user's spelling)", got, "Work-Travel,Reimbursable")
	}

	res, err := svc.Apply(ctx, userID, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Updated != 2 || res.SkippedManual != 1 {
		t.Errorf("Apply = %+v, want updated 2, skipped_manual 1", res)
	}
	tags := func(id int64) string {
		var got model.Transaction
		if err := g.First(&got, id).Error; err != nil {
			t.Fatalf("load %d: %v", id, err)
		}
		return strings.Join(got.Tags, ",")
	}
	for _, id := range []int64{a.ID, b.ID} {
		if got := tags(id); got != "Work-Travel,Reimbursable" {
			t.Errorf("txn %d tags = %q, want %q", id, got, "Work-Travel,Reimbursable")
		}
	}
	if got := tags(other.ID); got != "" {
		t.Errorf("non-matching row tags = %q, want none", got)
	}
	if got := tags(manual.ID); got != "" {
		t.Errorf("manual row tags = %q, want untouched", got)
	}

	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("re-Apply updated = %d, want 0 (idempotent)", res.Updated)
	}

	// Clearing the rule's tags keeps rows tagged; nothing for Apply to do.
	none := []string{}
	updated, err := svc.Update(ctx, userID, rule.ID, service.UpdateRuleInput{AddTags: &none})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated.AddTags) != 0 {
		t.Errorf("add_tags after clear = %v, want []", updated.AddTags)
	}
	if res, _ = svc.Apply(ctx, userID, ""); res.Updated != 0 {
		t.Errorf("Apply after clear updated = %d, want 0", res.Updated)
	}
	if got := tags(a.ID); got != "Work-Travel,Reimbursable" {
		t.Errorf("row tags after clear = %q, want kept", got)
	}
}

// TestCategorizationRuleService_Apply_ActionCounts (#552): preview and apply
// report per-action counts from the same decision diff, so a row that is
// already in the rule's category counts as renamed/hidden/reviewed/tagged
// but not re-categorized, and the draft preview agrees.
func TestCategorizationRuleService_Apply_ActionCounts(t *testing.T) {
	svc, userID, accountID, fixtureCat, g := newRuleApplySvc(t)
	ctx := context.Background()

	mkTxn(t, g, userID, accountID, withDescription("QACOUNT A"))
	mkTxn(t, g, userID, accountID, withDescription("QACOUNT B"), withPlaidDefaultCategory(fixtureCat))
	mkTxn(t, g, userID, accountID, withDescription("QACOUNT manual"), withManualCategory(fixtureCat))
	mkTxn(t, g, userID, accountID, withDescription("SHELL GAS"))

	rename := "Count Co"
	in := service.CreateRuleInput{
		Pattern: "QACOUNT", MatchType: "contains", CategoryID: new(fixtureCat), Priority: 50,
		RenameMerchant: &rename, HideMatches: true, MarkReviewed: true, AddTags: []string{"counted"},
	}
	draft, err := svc.PreviewDraft(ctx, userID, service.DraftRuleInput{
		Pattern: in.Pattern, MatchType: in.MatchType, CategoryID: in.CategoryID, Priority: in.Priority, IsActive: true,
		RenameMerchant: in.RenameMerchant, HideMatches: true, MarkReviewed: true, AddTags: in.AddTags,
	})
	if err != nil {
		t.Fatalf("PreviewDraft: %v", err)
	}
	if draft.WouldChange != 2 || draft.WouldRecategorize != 1 || draft.WouldRename != 2 {
		t.Errorf("draft = %+v, want would_change 2, would_recategorize 1, would_rename 2", draft)
	}

	rule, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	want := service.ApplyResult{
		Scanned: 4, Updated: 2, SkippedManual: 1,
		Recategorized: 1, Renamed: 2, Hidden: 2, MarkedReviewed: 2, Tagged: 2,
	}
	tests := []struct {
		name string
		run  func(context.Context, int64, string) (service.ApplyResult, error)
		want service.ApplyResult
	}{
		{"preview", svc.Preview, func() service.ApplyResult { w := want; w.DryRun = true; return w }()},
		{"apply", svc.Apply, want},
		{"re-apply is a no-op", svc.Apply, service.ApplyResult{Scanned: 4, SkippedManual: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run(ctx, userID, "")
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
