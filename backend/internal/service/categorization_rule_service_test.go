package service_test

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func newRuleSvc(t *testing.T) (*service.CategorizationRuleService, int64, *gorm.DB) {
	t.Helper()
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	return service.NewCategorizationRuleService(
		repository.NewCategorizationRuleRepository(g),
		repository.NewCategoryRepository(g),
	), userID, g
}

// firstSystemCategory returns the lowest-id system category id, which the
// seed migration guarantees exists. Tests need a real category to pass
// validation but don't care which one.
func firstSystemCategory(t *testing.T, g *gorm.DB) int64 {
	t.Helper()
	var c model.Category
	if err := g.Where("is_system = ?", true).Order("id ASC").First(&c).Error; err != nil {
		t.Fatalf("load seed category: %v", err)
	}
	return c.ID
}

func TestCategorizationRuleService_Create_Validation(t *testing.T) {
	svc, userID, g := newRuleSvc(t)
	ctx := context.Background()
	catID := firstSystemCategory(t, g)

	base := service.CreateRuleInput{
		Pattern:    "WHOLEFDS",
		MatchType:  "contains",
		CategoryID: catID,
		Priority:   10,
	}

	cases := []struct {
		name    string
		mutate  func(*service.CreateRuleInput)
		wantErr error
	}{
		{"valid", func(*service.CreateRuleInput) {}, nil},
		{"empty pattern", func(in *service.CreateRuleInput) { in.Pattern = "  " }, service.ErrEmptyPattern},
		{"bad match_type", func(in *service.CreateRuleInput) { in.MatchType = "bogus" }, service.ErrInvalidMatchType},
		{"invalid regex", func(in *service.CreateRuleInput) {
			in.MatchType = "regex"
			in.Pattern = "([unclosed"
		}, service.ErrInvalidRegex},
		{"valid regex", func(in *service.CreateRuleInput) {
			in.MatchType = "regex"
			in.Pattern = `^AMZN\s`
		}, nil},
		{"negative priority", func(in *service.CreateRuleInput) { in.Priority = -1 }, service.ErrInvalidPriority},
		{"unknown category", func(in *service.CreateRuleInput) { in.CategoryID = 9_999_999 }, service.ErrUnknownCategory},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			r, err := svc.Create(ctx, userID, in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("Create err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if r == nil || r.ID == 0 || r.UserID != userID {
				t.Fatalf("expected created rule, got %+v", r)
			}
			t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, r.ID) })
		})
	}
}

// TestCategorizationRuleService_List_OrderAndScope verifies ordering and
// soft-delete exclusion in one pass.
func TestCategorizationRuleService_List_OrderAndScope(t *testing.T) {
	svc, userID, g := newRuleSvc(t)
	ctx := context.Background()
	catID := firstSystemCategory(t, g)

	mk := func(pattern string, priority int) *model.CategorizationRule {
		r, err := svc.Create(ctx, userID, service.CreateRuleInput{
			Pattern: pattern, MatchType: "contains", CategoryID: catID, Priority: priority,
		})
		if err != nil {
			t.Fatalf("create %s: %v", pattern, err)
		}
		t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, r.ID) })
		return r
	}

	low := mk("low", 1)
	high := mk("high", 100)
	mid := mk("mid", 50)

	rules, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("got %d rules, want 3", len(rules))
	}
	if rules[0].ID != high.ID || rules[1].ID != mid.ID || rules[2].ID != low.ID {
		t.Errorf("unexpected order: %d, %d, %d (want %d, %d, %d)",
			rules[0].ID, rules[1].ID, rules[2].ID, high.ID, mid.ID, low.ID)
	}

	// Soft-delete the middle one and verify it disappears.
	if err := svc.SoftDelete(ctx, userID, mid.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	rules, err = svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("after soft-delete: got %d, want 2", len(rules))
	}
	for _, r := range rules {
		if r.ID == mid.ID {
			t.Errorf("soft-deleted rule still listed: %d", r.ID)
		}
	}
}

// TestCategorizationRuleService_TenantIsolation: user B cannot read or delete
// user A's rule.
func TestCategorizationRuleService_TenantIsolation(t *testing.T) {
	svc, userA, g := newRuleSvc(t)
	ctx := context.Background()
	catID := firstSystemCategory(t, g)

	r, err := svc.Create(ctx, userA, service.CreateRuleInput{
		Pattern: "TENANT-ISO", MatchType: "exact", CategoryID: catID, Priority: 5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userA, r.ID) })

	userB := seedTestUser(t, g)

	if _, err := svc.Get(ctx, userB, r.ID); !errors.Is(err, service.ErrRuleNotFound) {
		t.Errorf("cross-tenant Get err = %v, want ErrRuleNotFound", err)
	}
	if err := svc.SoftDelete(ctx, userB, r.ID); !errors.Is(err, service.ErrRuleNotFound) {
		t.Errorf("cross-tenant SoftDelete err = %v, want ErrRuleNotFound", err)
	}
	newPattern := "HACKED"
	if _, err := svc.Update(ctx, userB, r.ID, service.UpdateRuleInput{Pattern: &newPattern}); !errors.Is(err, service.ErrRuleNotFound) {
		t.Errorf("cross-tenant Update err = %v, want ErrRuleNotFound", err)
	}

	// List for user B must not include user A's rule.
	rules, err := svc.List(ctx, userB)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	for _, item := range rules {
		if item.ID == r.ID {
			t.Errorf("cross-tenant List leaked rule %d to user %d", item.ID, userB)
		}
	}
}

// TestCategorizationRuleService_Update_RevalidatesRegex makes sure an Update
// that flips match_type to regex with a bad pattern is rejected, the same
// way Create rejects it.
func TestCategorizationRuleService_Update_RevalidatesRegex(t *testing.T) {
	svc, userID, g := newRuleSvc(t)
	ctx := context.Background()
	catID := firstSystemCategory(t, g)

	r, err := svc.Create(ctx, userID, service.CreateRuleInput{
		Pattern: "WHOLEFDS", MatchType: "contains", CategoryID: catID, Priority: 10,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, r.ID) })

	regex := "regex"
	bad := "([unclosed"
	if _, err := svc.Update(ctx, userID, r.ID, service.UpdateRuleInput{
		MatchType: &regex,
		Pattern:   &bad,
	}); !errors.Is(err, service.ErrInvalidRegex) {
		t.Errorf("Update err = %v, want ErrInvalidRegex", err)
	}
}
