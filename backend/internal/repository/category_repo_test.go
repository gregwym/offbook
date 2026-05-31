package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// seedCategory inserts a user-owned category (user_id non-nil) and registers
// cleanup. Returns the new category id.
func seedCategory(t *testing.T, g *gorm.DB, userID int64, slug string) int64 {
	t.Helper()
	c := &model.Category{
		UserID: &userID,
		Name:   slug,
		Slug:   slug,
	}
	if err := g.Create(c).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, c.ID) })
	return c.ID
}

// TestCategoryRepo_List_ScopedByUser asserts List returns the system taxonomy
// plus the caller's own categories, and never another user's. This is the
// multi-tenant read guarantee for #285.
func TestCategoryRepo_List_ScopedByUser(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewCategoryRepository(g)
	ctx := context.Background()

	userA := seedTestUser(t, g)
	userB := seedTestUser(t, g)
	stamp := time.Now().UnixNano()
	catA := seedCategory(t, g, userA, fmt.Sprintf("cat-a-%d", stamp))
	catB := seedCategory(t, g, userB, fmt.Sprintf("cat-b-%d", stamp))

	listA, err := repo.List(ctx, userA)
	if err != nil {
		t.Fatalf("List(A): %v", err)
	}

	var sawA, sawB, sawSystem bool
	for _, c := range listA {
		switch {
		case c.ID == catA:
			sawA = true
		case c.ID == catB:
			sawB = true
		case c.UserID == nil:
			sawSystem = true
		}
	}
	if !sawA {
		t.Error("List(A) missing user A's own category")
	}
	if sawB {
		t.Error("List(A) leaked user B's private category — multi-tenant violation")
	}
	if !sawSystem {
		t.Error("List(A) missing seeded system categories (user_id NULL)")
	}

	// Symmetric check from B's side.
	listB, err := repo.List(ctx, userB)
	if err != nil {
		t.Fatalf("List(B): %v", err)
	}
	for _, c := range listB {
		if c.ID == catA {
			t.Error("List(B) leaked user A's private category — multi-tenant violation")
		}
	}
}
