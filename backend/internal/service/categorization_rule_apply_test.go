package service_test

import (
	"context"
	"errors"
	"fmt"
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
		Currency:        "USD",
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
	).WithBulkApply(repository.NewTransactionRepository(g), g)

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
		Currency:        "USD",
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
		CategoryID: fixtureCat, Priority: 50, IsActive: true,
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
		CategoryID: fixtureCat, Priority: 50, IsActive: true,
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
		CategoryID: fixtureCat, Priority: 50, IsActive: true,
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
