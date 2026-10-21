package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// TestCategorizationRuleService_Reorder covers #540: the order a user picks
// is the order List returns and the order the engine applies (first match
// wins), and an order that doesn't list each of the user's rules exactly
// once changes nothing.
func TestCategorizationRuleService_Reorder(t *testing.T) {
	_, userID, g := newRuleSvc(t)
	svc := service.NewCategorizationRuleService(
		repository.NewCategorizationRuleRepository(g),
		repository.NewCategoryRepository(g),
	).WithBulkApply(repository.NewTransactionRepository(g), g)
	ctx := context.Background()

	var cats []model.Category
	if err := g.Where("is_system = ?", true).Order("id ASC").Limit(2).Find(&cats).Error; err != nil || len(cats) < 2 {
		t.Fatalf("load two seed categories: %v", err)
	}
	mk := func(pattern string, catID int64, priority int) *model.CategorizationRule {
		r, err := svc.Create(ctx, userID, service.CreateRuleInput{
			Pattern: pattern, MatchType: "contains", CategoryID: new(catID), Priority: priority,
		})
		if err != nil {
			t.Fatalf("create %s: %v", pattern, err)
		}
		t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, r.ID) })
		return r
	}
	// Overlapping patterns: "AMAZON PRIME" matches both.
	broad := mk("AMAZON", cats[0].ID, 20)
	narrow := mk("AMAZON PRIME", cats[1].ID, 10)
	deleted := mk("GONE", cats[0].ID, 5)
	if err := svc.SoftDelete(ctx, userID, deleted.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	winner := func() int64 {
		t.Helper()
		rules, err := svc.List(ctx, userID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		desc := "AMAZON PRIME*2K4"
		d, ok := categorization.Categorize(categorization.Compile(rules), 0, nil, &desc, nil)
		if !ok {
			t.Fatalf("no rule matched")
		}
		return d.RuleID
	}
	if got := winner(); got != broad.ID {
		t.Fatalf("before reorder winner = %d, want broad %d", got, broad.ID)
	}

	t.Run("moves narrow rule first", func(t *testing.T) {
		rules, err := svc.Reorder(ctx, userID, []int64{narrow.ID, broad.ID})
		if err != nil {
			t.Fatalf("reorder: %v", err)
		}
		if len(rules) != 2 || rules[0].ID != narrow.ID || rules[1].ID != broad.ID {
			t.Fatalf("returned order = %+v, want [%d %d]", rules, narrow.ID, broad.ID)
		}
		if rules[0].Priority <= rules[1].Priority {
			t.Errorf("priorities %d, %d not strictly descending", rules[0].Priority, rules[1].Priority)
		}
		if got := winner(); got != narrow.ID {
			t.Errorf("after reorder winner = %d, want narrow %d", got, narrow.ID)
		}
	})

	otherUser := seedTestUser(t, g)
	foreign, err := svc.Create(ctx, otherUser, service.CreateRuleInput{
		Pattern: "FOREIGN", MatchType: "contains", CategoryID: new(cats[0].ID), Priority: 1,
	})
	if err != nil {
		t.Fatalf("create foreign: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, otherUser, foreign.ID) })

	bad := []struct {
		name string
		ids  []int64
	}{
		{"missing a rule", []int64{broad.ID}},
		{"duplicate", []int64{broad.ID, broad.ID}},
		{"extra rule", []int64{broad.ID, narrow.ID, broad.ID + narrow.ID + 1000}},
		{"another user's rule", []int64{broad.ID, foreign.ID}},
		{"deleted rule", []int64{broad.ID, deleted.ID}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Reorder(ctx, userID, tc.ids); !errors.Is(err, service.ErrInvalidRuleOrder) {
				t.Fatalf("err = %v, want ErrInvalidRuleOrder", err)
			}
			rules, err := svc.List(ctx, userID)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(rules) != 2 || rules[0].ID != narrow.ID || rules[1].ID != broad.ID {
				t.Errorf("rejected reorder changed the order: %+v", rules)
			}
		})
	}

	t.Run("other user's order untouched", func(t *testing.T) {
		got, err := svc.Get(ctx, otherUser, foreign.ID)
		if err != nil {
			t.Fatalf("get foreign: %v", err)
		}
		if got.Priority != 1 {
			t.Errorf("foreign priority = %d, want 1", got.Priority)
		}
	})
}
