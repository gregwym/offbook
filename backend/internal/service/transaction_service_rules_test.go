package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestTransactionService_Create_AppliesRule covers the happy path: when
// the caller didn't supply a CategoryID, the user's matching rule fires
// and stamps category + method=rule + rule_id on the new row.
func TestTransactionService_Create_AppliesRule(t *testing.T) {
	svc, userID, accountID, categoryID, g := newTxSvc(t)
	ctx := context.Background()

	// Wire ruleRepo onto the service for this test.
	ruleRepo := repository.NewCategorizationRuleRepository(g)
	svc = svc.WithRuleRepo(ruleRepo)

	// Seed a rule for the user: any description containing WHOLEFDS → the
	// fixture category.
	rule := &model.CategorizationRule{
		UserID:     userID,
		Pattern:    "WHOLEFDS",
		MatchType:  "contains",
		CategoryID: categoryID,
		Priority:   10,
		IsActive:   true,
	}
	if err := g.WithContext(ctx).Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	desc := "WHOLEFDS MARKET 123"
	in := service.CreateTransactionInput{
		AccountID:       accountID,
		Amount:          decimal.NewFromFloat(-50),
		Description:     &desc,
		TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	}
	tx, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, tx.ID) })

	if tx.CategoryID == nil || *tx.CategoryID != categoryID {
		t.Errorf("CategoryID = %v, want %d", tx.CategoryID, categoryID)
	}
	if tx.CategorizationMethod == nil || *tx.CategorizationMethod != "rule" {
		t.Errorf("CategorizationMethod = %v, want rule", tx.CategorizationMethod)
	}
	if tx.CategorizationRuleID == nil || *tx.CategorizationRuleID != rule.ID {
		t.Errorf("CategorizationRuleID = %v, want %d", tx.CategorizationRuleID, rule.ID)
	}
}

// TestTransactionService_Create_RuleDoesNotCrossTenant: user B's rule
// must not categorize user A's transaction.
func TestTransactionService_Create_RuleDoesNotCrossTenant(t *testing.T) {
	svc, userA, accountID, categoryID, g := newTxSvc(t)
	ctx := context.Background()

	ruleRepo := repository.NewCategorizationRuleRepository(g)
	svc = svc.WithRuleRepo(ruleRepo)

	// User B has a rule that would otherwise match user A's transaction.
	userB := seedTestUser(t, g)
	rule := &model.CategorizationRule{
		UserID:     userB,
		Pattern:    "WHOLEFDS",
		MatchType:  "contains",
		CategoryID: categoryID,
		Priority:   10,
		IsActive:   true,
	}
	if err := g.WithContext(ctx).Create(rule).Error; err != nil {
		t.Fatalf("seed cross-tenant rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	desc := "WHOLEFDS MARKET 123"
	tx, err := svc.Create(ctx, userA, service.CreateTransactionInput{
		AccountID:       accountID,
		Amount:          decimal.NewFromFloat(-50),
		Description:     &desc,
		TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userA, tx.ID) })

	if tx.CategoryID != nil {
		t.Errorf("CategoryID = %v, want nil (rule belonged to user B)", tx.CategoryID)
	}
	if tx.CategorizationMethod != nil {
		t.Errorf("CategorizationMethod = %v, want nil", tx.CategorizationMethod)
	}
	if tx.CategorizationRuleID != nil {
		t.Errorf("CategorizationRuleID = %v, want nil", tx.CategorizationRuleID)
	}
}

// TestTransactionService_Create_ManualCategoryWinsOverRule: when the user
// supplies a CategoryID, the row is method=manual even if a rule would
// otherwise match.
func TestTransactionService_Create_ManualCategoryWinsOverRule(t *testing.T) {
	svc, userID, accountID, categoryID, g := newTxSvc(t)
	ctx := context.Background()

	ruleRepo := repository.NewCategorizationRuleRepository(g)
	svc = svc.WithRuleRepo(ruleRepo)

	// Need a second category so the rule's target differs from the user-
	// supplied one — verifies the rule was skipped, not just no-op'd.
	other := &model.Category{
		Name:     "OtherFixture",
		Slug:     "other-fixture-" + time.Now().Format("150405.000000"),
		IsSystem: false,
	}
	if err := g.WithContext(ctx).Create(other).Error; err != nil {
		t.Fatalf("seed other category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, other.ID) })

	rule := &model.CategorizationRule{
		UserID:     userID,
		Pattern:    "WHOLEFDS",
		MatchType:  "contains",
		CategoryID: other.ID,
		Priority:   10,
		IsActive:   true,
	}
	if err := g.WithContext(ctx).Create(rule).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.CategorizationRule{}, rule.ID) })

	desc := "WHOLEFDS MARKET 123"
	pickedCat := categoryID
	tx, err := svc.Create(ctx, userID, service.CreateTransactionInput{
		AccountID:       accountID,
		CategoryID:      &pickedCat,
		Amount:          decimal.NewFromFloat(-50),
		Description:     &desc,
		TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, tx.ID) })

	if tx.CategoryID == nil || *tx.CategoryID != categoryID {
		t.Errorf("CategoryID = %v, want %d (user pick)", tx.CategoryID, categoryID)
	}
	if tx.CategorizationMethod == nil || *tx.CategorizationMethod != "manual" {
		t.Errorf("CategorizationMethod = %v, want manual", tx.CategorizationMethod)
	}
	if tx.CategorizationRuleID != nil {
		t.Errorf("CategorizationRuleID = %v, want nil (user pick should not set rule_id)", tx.CategorizationRuleID)
	}
}

// TestTransactionService_Create_PriorityBreaksTie: two rules match; higher
// priority wins. Lower id is the tie-break (covered by the engine; this
// test verifies the wiring against real Postgres data.)
func TestTransactionService_Create_PriorityBreaksTie(t *testing.T) {
	svc, userID, accountID, categoryID, g := newTxSvc(t)
	ctx := context.Background()

	ruleRepo := repository.NewCategorizationRuleRepository(g)
	svc = svc.WithRuleRepo(ruleRepo)

	other := &model.Category{
		Name:     "PriorityFixture",
		Slug:     "priority-fixture-" + time.Now().Format("150405.000000"),
		IsSystem: false,
	}
	if err := g.WithContext(ctx).Create(other).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, other.ID) })

	low := &model.CategorizationRule{
		UserID: userID, Pattern: "WHOLEFDS", MatchType: "contains",
		CategoryID: categoryID, Priority: 1, IsActive: true,
	}
	high := &model.CategorizationRule{
		UserID: userID, Pattern: "WHOLEFDS", MatchType: "contains",
		CategoryID: other.ID, Priority: 100, IsActive: true,
	}
	if err := g.WithContext(ctx).Create(low).Error; err != nil {
		t.Fatalf("seed low: %v", err)
	}
	if err := g.WithContext(ctx).Create(high).Error; err != nil {
		t.Fatalf("seed high: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.CategorizationRule{}, low.ID)
		g.Unscoped().Delete(&model.CategorizationRule{}, high.ID)
	})

	desc := "WHOLEFDS MARKET"
	tx, err := svc.Create(ctx, userID, service.CreateTransactionInput{
		AccountID:       accountID,
		Amount:          decimal.NewFromFloat(-50),
		Description:     &desc,
		TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, tx.ID) })

	if tx.CategorizationRuleID == nil || *tx.CategorizationRuleID != high.ID {
		t.Errorf("matched rule = %v, want %d (higher priority)", tx.CategorizationRuleID, high.ID)
	}
	if tx.CategoryID == nil || *tx.CategoryID != other.ID {
		t.Errorf("CategoryID = %v, want %d", tx.CategoryID, other.ID)
	}
}
