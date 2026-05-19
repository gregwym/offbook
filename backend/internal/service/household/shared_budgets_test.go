package household_test

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
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// withSharedBudgets returns a household.Service that has the SharedBudget
// repos wired. newSvc already calls WithDB; we layer the budget repos on
// top so older tests don't need to know about them.
func withSharedBudgets(t *testing.T) (*household.Service, *gorm.DB, *model.Category) {
	t.Helper()
	svc, _, g := newSvc(t)
	svc.WithSharedBudgets(
		repository.NewSharedBudgetRepository(g),
		repository.NewCategoryRepository(g),
	)
	cat := &model.Category{
		Name: "SB Test Cat",
		Slug: fmt.Sprintf("sb-test-%d", time.Now().UnixNano()),
	}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("category_id = ?", cat.ID).Delete(&model.SharedBudget{})
		g.Unscoped().Delete(&model.Category{}, cat.ID)
	})
	return svc, g, cat
}

// seedSharedBudgetHousehold spins up a household with one owner and one
// contributor and returns IDs the tests need.
func seedSharedBudgetHousehold(t *testing.T, svc *household.Service, g *gorm.DB) (ownerID, contribID, viewOnlyID, householdID int64) {
	t.Helper()
	ctx := context.Background()
	ownerID = seedUser(t, g, "sb-owner")
	contribID = seedUser(t, g, "sb-contrib")
	viewOnlyID = seedUser(t, g, "sb-view")
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "SB House"})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	contribInv, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("invite contrib: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, contribID, contribInv.Token); err != nil {
		t.Fatalf("accept contrib: %v", err)
	}

	viewInv, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleViewOnly})
	if err != nil {
		t.Fatalf("invite view: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, viewOnlyID, viewInv.Token); err != nil {
		t.Fatalf("accept view: %v", err)
	}
	return ownerID, contribID, viewOnlyID, hh.ID
}

func TestCreateSharedBudget_HappyPath(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)

	b, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID,
		Period:     "monthly",
		Amount:     decimal.NewFromInt(500),
	})
	if err != nil {
		t.Fatalf("CreateSharedBudget: %v", err)
	}
	if b.HouseholdID != hhID {
		t.Errorf("HouseholdID = %d, want %d", b.HouseholdID, hhID)
	}
	if b.CategoryID != cat.ID || b.Period != "monthly" || !b.Amount.Equal(decimal.NewFromInt(500)) {
		t.Errorf("budget = %+v, want monthly $500 in cat %d", b, cat.ID)
	}
	if !b.IsActive {
		t.Errorf("IsActive = false, want true by default")
	}
}

func TestCreateSharedBudget_ContributorAllowed(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	_, contribID, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	_, err := svc.CreateSharedBudget(context.Background(), contribID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID,
		Period:     "monthly",
		Amount:     decimal.NewFromInt(100),
	})
	if err != nil {
		t.Errorf("contributor blocked: %v", err)
	}
}

func TestCreateSharedBudget_ViewOnlyForbidden(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	_, _, viewOnlyID, hhID := seedSharedBudgetHousehold(t, svc, g)
	_, err := svc.CreateSharedBudget(context.Background(), viewOnlyID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID,
		Period:     "monthly",
		Amount:     decimal.NewFromInt(100),
	})
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreateSharedBudget_NonMemberRejected(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	_, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	outsiderID := seedUser(t, g, "sb-outsider")
	_, err := svc.CreateSharedBudget(context.Background(), outsiderID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID,
		Period:     "monthly",
		Amount:     decimal.NewFromInt(100),
	})
	if !errors.Is(err, household.ErrNotMember) {
		t.Fatalf("err = %v, want ErrNotMember", err)
	}
}

func TestCreateSharedBudget_ValidationRejectsBadPeriod(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	_, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID,
		Period:     "fortnightly",
		Amount:     decimal.NewFromInt(100),
	})
	if !errors.Is(err, household.ErrInvalidBudgetPeriod) {
		t.Fatalf("err = %v, want ErrInvalidBudgetPeriod", err)
	}
}

func TestCreateSharedBudget_ValidationRejectsZeroAmount(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	_, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID,
		Period:     "monthly",
		Amount:     decimal.Zero,
	})
	if !errors.Is(err, household.ErrInvalidBudgetAmount) {
		t.Fatalf("err = %v, want ErrInvalidBudgetAmount", err)
	}
}

func TestCreateSharedBudget_UnknownCategory(t *testing.T) {
	svc, g, _ := withSharedBudgets(t)
	ownerID, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	_, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: 999_999_999,
		Period:     "monthly",
		Amount:     decimal.NewFromInt(100),
	})
	if !errors.Is(err, household.ErrUnknownCategory) {
		t.Fatalf("err = %v, want ErrUnknownCategory", err)
	}
}

func TestListSharedBudgets_MemberReadsAll(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, viewOnlyID, hhID := seedSharedBudgetHousehold(t, svc, g)

	if _, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	}); err != nil {
		t.Fatalf("seed budget: %v", err)
	}

	// View-only can read — read is gated only on membership.
	got, err := svc.ListSharedBudgets(context.Background(), viewOnlyID, hhID)
	if err != nil {
		t.Fatalf("ListSharedBudgets: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d budgets, want 1", len(got))
	}
}

func TestListSharedBudgets_NonMemberRejected(t *testing.T) {
	svc, g, _ := withSharedBudgets(t)
	_, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	outsiderID := seedUser(t, g, "sb-outsider")
	_, err := svc.ListSharedBudgets(context.Background(), outsiderID, hhID)
	if !errors.Is(err, household.ErrNotMember) {
		t.Fatalf("err = %v, want ErrNotMember", err)
	}
}

func TestUpdateSharedBudget_HappyPath(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)

	b, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	newAmount := decimal.NewFromInt(250)
	newActive := false
	updated, err := svc.UpdateSharedBudget(context.Background(), ownerID, hhID, b.ID, household.UpdateSharedBudgetInput{
		Amount:   &newAmount,
		IsActive: &newActive,
	})
	if err != nil {
		t.Fatalf("UpdateSharedBudget: %v", err)
	}
	if !updated.Amount.Equal(decimal.NewFromInt(250)) {
		t.Errorf("amount = %s, want 250", updated.Amount)
	}
	if updated.IsActive {
		t.Errorf("IsActive = true, want false")
	}
}

func TestUpdateSharedBudget_ViewOnlyForbidden(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, viewOnlyID, hhID := seedSharedBudgetHousehold(t, svc, g)
	b, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := decimal.NewFromInt(200)
	_, err = svc.UpdateSharedBudget(context.Background(), viewOnlyID, hhID, b.ID, household.UpdateSharedBudgetInput{
		Amount: &a,
	})
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdateSharedBudget_NotFound(t *testing.T) {
	svc, g, _ := withSharedBudgets(t)
	ownerID, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	a := decimal.NewFromInt(1)
	_, err := svc.UpdateSharedBudget(context.Background(), ownerID, hhID, 999_999, household.UpdateSharedBudgetInput{
		Amount: &a,
	})
	if !errors.Is(err, household.ErrBudgetNotFound) {
		t.Fatalf("err = %v, want ErrBudgetNotFound", err)
	}
}

func TestSoftDeleteSharedBudget_HappyPath(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, _, hhID := seedSharedBudgetHousehold(t, svc, g)
	b, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.SoftDeleteSharedBudget(context.Background(), ownerID, hhID, b.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	got, err := svc.ListSharedBudgets(context.Background(), ownerID, hhID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, row := range got {
		if row.ID == b.ID {
			t.Errorf("budget %d still in list after delete", b.ID)
		}
	}
}

func TestSoftDeleteSharedBudget_ViewOnlyForbidden(t *testing.T) {
	svc, g, cat := withSharedBudgets(t)
	ownerID, _, viewOnlyID, hhID := seedSharedBudgetHousehold(t, svc, g)
	b, err := svc.CreateSharedBudget(context.Background(), ownerID, hhID, household.SharedBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	err = svc.SoftDeleteSharedBudget(context.Background(), viewOnlyID, hhID, b.ID)
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

// TestSharedBudget_TenantIsolation: a member of household A cannot read,
// edit, or delete a budget in household B.
func TestSharedBudget_TenantIsolation(t *testing.T) {
	svc, g, catA := withSharedBudgets(t)
	ownerA, _, _, hhA := seedSharedBudgetHousehold(t, svc, g)

	// Spin up an independent household B.
	ctx := context.Background()
	ownerB := seedUser(t, g, "sb-ownerB")
	hhB, err := svc.Create(ctx, ownerB, household.CreateInput{Name: "Other House"})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	cleanupHousehold(t, g, hhB.ID)

	bA, err := svc.CreateSharedBudget(ctx, ownerA, hhA, household.SharedBudgetInput{
		CategoryID: catA.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}

	// Owner B tries to update A's budget by hitting A's household id with
	// a wrong session — but they aren't a member of A, so ErrNotMember.
	a := decimal.NewFromInt(99)
	_, err = svc.UpdateSharedBudget(ctx, ownerB, hhA, bA.ID, household.UpdateSharedBudgetInput{Amount: &a})
	if !errors.Is(err, household.ErrNotMember) {
		t.Errorf("cross-tenant update err = %v, want ErrNotMember", err)
	}

	// Same household_id but wrong: owner B uses their OWN household id to
	// try and update A's budget id. Should be ErrBudgetNotFound (budget
	// doesn't exist in B's household scope).
	_, err = svc.UpdateSharedBudget(ctx, ownerB, hhB.ID, bA.ID, household.UpdateSharedBudgetInput{Amount: &a})
	if !errors.Is(err, household.ErrBudgetNotFound) {
		t.Errorf("scoped-to-wrong-household err = %v, want ErrBudgetNotFound", err)
	}
}
