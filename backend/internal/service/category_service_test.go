package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// systemCategoryID returns a seeded system category (migration 000002).
func systemCategoryID(t *testing.T, slug string) int64 {
	t.Helper()
	g := openTestDB(t)
	var c model.Category
	if err := g.Where("user_id IS NULL AND slug = ?", slug).First(&c).Error; err != nil {
		t.Fatalf("system category %q: %v", slug, err)
	}
	return c.ID
}

// TestCategoryService_CreateRename covers #573: a user adds and renames their
// own categories; names stay unique across what they can see; system and
// other users' categories can't be renamed.
func TestCategoryService_CreateRename(t *testing.T) {
	ctx := context.Background()
	g := openTestDB(t)
	svc := service.NewCategoryService(repository.NewCategoryRepository(g))
	alice := seedTestUser(t, g)
	bob := seedTestUser(t, g)
	food := systemCategoryID(t, "food-and-dining")

	coffee, err := svc.Create(ctx, alice, service.CreateCategoryInput{Name: "  Coffee   shops ", ParentID: &food})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if coffee.Name != "Coffee shops" || coffee.Slug != "coffee-shops" || *coffee.UserID != alice || *coffee.ParentID != food || coffee.IsSystem {
		t.Fatalf("created = %+v", coffee)
	}

	t.Run("listed for the owner only", func(t *testing.T) {
		mine, _ := svc.List(ctx, alice)
		theirs, _ := svc.List(ctx, bob)
		if !hasCategory(mine, coffee.ID) {
			t.Error("owner's list is missing the new category")
		}
		if hasCategory(theirs, coffee.ID) {
			t.Error("another user's list shows it — multi-tenant violation")
		}
	})

	t.Run("bob may reuse the name in his own namespace", func(t *testing.T) {
		if _, err := svc.Create(ctx, bob, service.CreateCategoryInput{Name: "Coffee shops"}); err != nil {
			t.Fatalf("bob Create: %v", err)
		}
	})

	t.Run("slug collision gets a suffix", func(t *testing.T) {
		c, err := svc.Create(ctx, alice, service.CreateCategoryInput{Name: "Coffee-shops!"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if c.Slug != "coffee-shops-2" {
			t.Errorf("slug = %q, want coffee-shops-2", c.Slug)
		}
	})

	errCases := []struct {
		name string
		in   service.CreateCategoryInput
		want error
	}{
		{"blank name", service.CreateCategoryInput{Name: "   "}, service.ErrInvalidCategoryName},
		{"name too long", service.CreateCategoryInput{Name: strings.Repeat("é", 61)}, service.ErrInvalidCategoryName},
		{"duplicate of own, any case", service.CreateCategoryInput{Name: "COFFEE SHOPS"}, service.ErrCategoryNameTaken},
		{"duplicate of a system category", service.CreateCategoryInput{Name: "groceries"}, service.ErrCategoryNameTaken},
		{"parent is a child category", service.CreateCategoryInput{Name: "Espresso", ParentID: &coffee.ID}, service.ErrInvalidParent},
		{"parent does not exist", service.CreateCategoryInput{Name: "Espresso", ParentID: new(int64(-1))}, service.ErrInvalidParent},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(ctx, alice, tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("Create err = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("parent owned by another user is rejected", func(t *testing.T) {
		bobs, err := svc.Create(ctx, bob, service.CreateCategoryInput{Name: "Bob only"})
		if err != nil {
			t.Fatalf("bob Create: %v", err)
		}
		if _, err := svc.Create(ctx, alice, service.CreateCategoryInput{Name: "Sneaky", ParentID: &bobs.ID}); !errors.Is(err, service.ErrInvalidParent) {
			t.Fatalf("err = %v, want ErrInvalidParent", err)
		}
		if _, err := svc.Rename(ctx, alice, bobs.ID, "Mine now"); !errors.Is(err, service.ErrCategoryNotFound) {
			t.Fatalf("rename other's err = %v, want ErrCategoryNotFound", err)
		}
	})

	t.Run("rename own keeps the slug", func(t *testing.T) {
		c, err := svc.Rename(ctx, alice, coffee.ID, "Coffee")
		if err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if c.Name != "Coffee" || c.Slug != "coffee-shops" {
			t.Errorf("renamed = %q/%q", c.Name, c.Slug)
		}
		// Re-saving the same name (only case changes) is not a duplicate.
		if _, err := svc.Rename(ctx, alice, coffee.ID, "coffee"); err != nil {
			t.Errorf("case-only rename: %v", err)
		}
	})

	t.Run("rename errors", func(t *testing.T) {
		if _, err := svc.Rename(ctx, alice, food, "Eating"); !errors.Is(err, service.ErrCategoryReadOnly) {
			t.Errorf("system rename err = %v, want ErrCategoryReadOnly", err)
		}
		if _, err := svc.Rename(ctx, alice, coffee.ID, "Travel"); !errors.Is(err, service.ErrCategoryNameTaken) {
			t.Errorf("dup rename err = %v, want ErrCategoryNameTaken", err)
		}
		if _, err := svc.Rename(ctx, alice, coffee.ID, ""); !errors.Is(err, service.ErrInvalidCategoryName) {
			t.Errorf("blank rename err = %v, want ErrInvalidCategoryName", err)
		}
	})
}

func hasCategory(cs []model.Category, id int64) bool {
	for _, c := range cs {
		if c.ID == id {
			return true
		}
	}
	return false
}

// TestCategoryOwnership_WritePaths covers #573: every write that stores a
// category id accepts the user's own category and rejects another user's.
func TestCategoryOwnership_WritePaths(t *testing.T) {
	ctx := context.Background()
	svc, userID, accountID, _, g := newTxSvc(t)
	catRepo := repository.NewCategoryRepository(g)
	catSvc := service.NewCategoryService(catRepo)
	other := seedTestUser(t, g)
	// Registered last so it runs first: transactions reference the categories.
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Transaction{}) })

	mine, err := catSvc.Create(ctx, userID, service.CreateCategoryInput{Name: "Pets"})
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	theirs, err := catSvc.Create(ctx, other, service.CreateCategoryInput{Name: "Therapy"})
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}

	create := func(cat int64) error {
		_, err := svc.Create(ctx, userID, service.CreateTransactionInput{
			AccountID: accountID, CategoryID: &cat, Amount: decimal.NewFromInt(-12),
			Description: new("Vet"), TransactionDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), Source: "manual",
		})
		return err
	}
	t.Run("transaction create", func(t *testing.T) {
		if err := create(mine.ID); err != nil {
			t.Errorf("own category: %v", err)
		}
		if err := create(theirs.ID); !errors.Is(err, service.ErrInvalidCategory) {
			t.Errorf("other's category err = %v, want ErrInvalidCategory", err)
		}
	})

	row := mkTxn(t, g, userID, accountID)
	t.Run("transaction update", func(t *testing.T) {
		if _, err := svc.Update(ctx, userID, row.ID, service.UpdateTransactionInput{CategoryID: &theirs.ID}); !errors.Is(err, service.ErrInvalidCategory) {
			t.Errorf("err = %v, want ErrInvalidCategory", err)
		}
		if _, err := svc.Update(ctx, userID, row.ID, service.UpdateTransactionInput{CategoryID: &mine.ID}); err != nil {
			t.Errorf("own category: %v", err)
		}
	})

	t.Run("bulk edit and its undo", func(t *testing.T) {
		if _, err := svc.BulkEdit(ctx, userID, []int64{row.ID}, service.BulkEditInput{CategoryID: &theirs.ID}); !errors.Is(err, service.ErrInvalidCategory) {
			t.Errorf("bulk edit err = %v, want ErrInvalidCategory", err)
		}
		var cur model.Transaction
		if err := g.First(&cur, row.ID).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		forged := []repository.BulkEditPrior{{ID: row.ID, CategoryID: &theirs.ID, UpdatedAt: cur.UpdatedAt}}
		if _, err := svc.BulkUndo(ctx, userID, forged); !errors.Is(err, service.ErrInvalidCategory) {
			t.Errorf("forged undo err = %v, want ErrInvalidCategory", err)
		}
		missing := []repository.BulkEditPrior{{ID: row.ID, CategoryID: new(int64(-1)), UpdatedAt: cur.UpdatedAt}}
		if _, err := svc.BulkUndo(ctx, userID, missing); !errors.Is(err, service.ErrInvalidCategory) {
			t.Errorf("unknown-category undo err = %v, want ErrInvalidCategory (400, not 500)", err)
		}
		if err := g.First(&cur, row.ID).Error; err != nil || cur.CategoryID == nil || *cur.CategoryID != mine.ID {
			t.Errorf("row category changed by a rejected undo: %v", cur.CategoryID)
		}
	})

	t.Run("rules", func(t *testing.T) {
		rules := service.NewCategorizationRuleService(repository.NewCategorizationRuleRepository(g), catRepo)
		if _, err := rules.Create(ctx, userID, service.CreateRuleInput{Pattern: "VET", MatchType: "contains", CategoryID: &theirs.ID}); !errors.Is(err, service.ErrUnknownCategory) {
			t.Errorf("other's category err = %v, want ErrUnknownCategory", err)
		}
		if _, err := rules.Create(ctx, userID, service.CreateRuleInput{Pattern: "VET", MatchType: "contains", CategoryID: &mine.ID}); err != nil {
			t.Errorf("own category: %v", err)
		}
	})

	t.Run("budgets", func(t *testing.T) {
		budgets := service.NewBudgetService(repository.NewBudgetRepository(g), catRepo)
		in := func(cat int64) service.CreateBudgetInput {
			return service.CreateBudgetInput{CategoryID: cat, Period: "monthly", Amount: decimal.NewFromInt(100)}
		}
		if _, err := budgets.Create(ctx, repository.UserOwner(userID), in(theirs.ID)); !errors.Is(err, service.ErrUnknownCategory) {
			t.Errorf("other's category err = %v, want ErrUnknownCategory", err)
		}
		if _, err := budgets.Create(ctx, repository.UserOwner(userID), in(mine.ID)); err != nil {
			t.Errorf("own category: %v", err)
		}
		// A household budget is seen by every member: system categories only.
		if _, err := budgets.Create(ctx, repository.HouseholdOwner(1), in(mine.ID)); !errors.Is(err, service.ErrUnknownCategory) {
			t.Errorf("household budget on a personal category err = %v, want ErrUnknownCategory", err)
		}
	})
}
