package household_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// withSharedGoals wires the unified goal service on top of newSvc's defaults.
func withSharedGoals(t *testing.T) (*household.Service, *gorm.DB) {
	t.Helper()
	svc, _, g := newSvc(t)
	goalSvc := service.NewSavingsGoalService(
		repository.NewSavingsGoalRepository(g),
		repository.NewAccountRepository(g),
	)
	svc.WithSharedGoals(goalSvc)
	return svc, g
}

// seedSharedGoalHousehold mirrors the shared-budget seeder: owner +
// contributor + view-only on one household.
func seedSharedGoalHousehold(t *testing.T, svc *household.Service, g *gorm.DB) (ownerID, contribID, viewOnlyID, hhID int64) {
	t.Helper()
	ctx := context.Background()
	ownerID = seedUser(t, g, "sg-owner")
	contribID = seedUser(t, g, "sg-contrib")
	viewOnlyID = seedUser(t, g, "sg-view")
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "SG House"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	t.Cleanup(func() {
		g.Unscoped().Where("household_id = ?", hh.ID).Delete(&model.SavingsGoal{})
	})

	ci, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("invite contrib: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, contribID, ci.Token); err != nil {
		t.Fatalf("accept contrib: %v", err)
	}
	vi, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleViewOnly})
	if err != nil {
		t.Fatalf("invite view: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, viewOnlyID, vi.Token); err != nil {
		t.Fatalf("accept view: %v", err)
	}
	return ownerID, contribID, viewOnlyID, hh.ID
}

func TestCreateSharedGoal_HappyPath(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	g_, err := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name:         "Vacation Fund",
		TargetAmount: decimal.NewFromInt(2000),
	})
	if err != nil {
		t.Fatalf("CreateSharedGoal: %v", err)
	}
	if g_.HouseholdID == nil || *g_.HouseholdID != hhID || g_.Name != "Vacation Fund" {
		t.Errorf("goal = %+v, want hhID=%d name=Vacation Fund", g_, hhID)
	}
	if g_.UserID != nil {
		t.Errorf("UserID = %v, want nil for a household goal", g_.UserID)
	}
	if !g_.CurrentAmount.IsZero() {
		t.Errorf("CurrentAmount = %s, want 0 at creation", g_.CurrentAmount)
	}
}

func TestCreateSharedGoal_ViewOnlyForbidden(t *testing.T) {
	svc, g := withSharedGoals(t)
	_, _, viewOnlyID, hhID := seedSharedGoalHousehold(t, svc, g)
	_, err := svc.CreateSharedGoal(context.Background(), viewOnlyID, hhID, household.SharedGoalInput{
		Name:         "Nope",
		TargetAmount: decimal.NewFromInt(100),
	})
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreateSharedGoal_ValidationRejectsEmptyName(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	_, err := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name:         "  ",
		TargetAmount: decimal.NewFromInt(100),
	})
	if !errors.Is(err, household.ErrSharedGoalEmptyName) {
		t.Fatalf("err = %v, want ErrSharedGoalEmptyName", err)
	}
}

func TestCreateSharedGoal_ValidationRejectsNonPositiveTarget(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	_, err := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name:         "Bad",
		TargetAmount: decimal.Zero,
	})
	if !errors.Is(err, household.ErrSharedGoalInvalidTarget) {
		t.Fatalf("err = %v, want ErrSharedGoalInvalidTarget", err)
	}
}

func TestListSharedGoals_NonMemberRejected(t *testing.T) {
	svc, g := withSharedGoals(t)
	_, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	outsiderID := seedUser(t, g, "sg-outsider")
	_, err := svc.ListSharedGoals(context.Background(), outsiderID, hhID)
	if !errors.Is(err, household.ErrNotMember) {
		t.Fatalf("err = %v, want ErrNotMember", err)
	}
}

func TestUpdateSharedGoal_HappyPath(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	created, err := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name: "Old Name", TargetAmount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	newName := "New Name"
	newTarget := decimal.NewFromInt(500)
	td := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	updated, err := svc.UpdateSharedGoal(context.Background(), ownerID, hhID, created.ID, household.UpdateSharedGoalInput{
		Name:         &newName,
		TargetAmount: &newTarget,
		TargetDate:   &td,
	})
	if err != nil {
		t.Fatalf("UpdateSharedGoal: %v", err)
	}
	if updated.Name != "New Name" || !updated.TargetAmount.Equal(decimal.NewFromInt(500)) {
		t.Errorf("update miss: %+v", updated)
	}
	if updated.TargetDate == nil || !updated.TargetDate.Equal(td) {
		t.Errorf("TargetDate = %v, want %v", updated.TargetDate, td)
	}
}

func TestUpdateSharedGoal_ClearTargetDate(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	td := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	created, err := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name: "Has Date", TargetAmount: decimal.NewFromInt(100), TargetDate: &td,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	updated, err := svc.UpdateSharedGoal(context.Background(), ownerID, hhID, created.ID, household.UpdateSharedGoalInput{
		ClearTargetDate: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.TargetDate != nil {
		t.Errorf("TargetDate = %v, want nil after clear", updated.TargetDate)
	}
}

func TestContributeToSharedGoal_HappyPath(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, contribID, _, hhID := seedSharedGoalHousehold(t, svc, g)
	created, err := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name: "Pool", TargetAmount: decimal.NewFromInt(1000),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Contributor adds 200.
	g_, err := svc.ContributeToSharedGoal(context.Background(), contribID, hhID, created.ID, decimal.NewFromInt(200))
	if err != nil {
		t.Fatalf("contribute +200: %v", err)
	}
	if !g_.CurrentAmount.Equal(decimal.NewFromInt(200)) {
		t.Errorf("after +200 current = %s, want 200", g_.CurrentAmount)
	}
	// Owner withdraws 50.
	g_, err = svc.ContributeToSharedGoal(context.Background(), ownerID, hhID, created.ID, decimal.NewFromInt(-50))
	if err != nil {
		t.Fatalf("contribute -50: %v", err)
	}
	if !g_.CurrentAmount.Equal(decimal.NewFromInt(150)) {
		t.Errorf("after -50 current = %s, want 150", g_.CurrentAmount)
	}
}

func TestContributeToSharedGoal_ZeroRejected(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	created, err := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name: "Pool", TargetAmount: decimal.NewFromInt(1000),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err = svc.ContributeToSharedGoal(context.Background(), ownerID, hhID, created.ID, decimal.Zero)
	if !errors.Is(err, household.ErrSharedGoalZeroContribution) {
		t.Fatalf("err = %v, want ErrSharedGoalZeroContribution", err)
	}
}

func TestContributeToSharedGoal_ViewOnlyForbidden(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, viewOnlyID, hhID := seedSharedGoalHousehold(t, svc, g)
	created, _ := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name: "X", TargetAmount: decimal.NewFromInt(100),
	})
	_, err := svc.ContributeToSharedGoal(context.Background(), viewOnlyID, hhID, created.ID, decimal.NewFromInt(10))
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestSoftDeleteSharedGoal_HappyPath(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerID, _, _, hhID := seedSharedGoalHousehold(t, svc, g)
	created, _ := svc.CreateSharedGoal(context.Background(), ownerID, hhID, household.SharedGoalInput{
		Name: "Bye", TargetAmount: decimal.NewFromInt(100),
	})
	if err := svc.SoftDeleteSharedGoal(context.Background(), ownerID, hhID, created.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	list, _ := svc.ListSharedGoals(context.Background(), ownerID, hhID)
	for _, gg := range list {
		if gg.ID == created.ID {
			t.Errorf("goal %d still listed after delete", created.ID)
		}
	}
}

// TestSharedGoal_TenantIsolation: a member of household A cannot mutate
// a goal in household B.
func TestSharedGoal_TenantIsolation(t *testing.T) {
	svc, g := withSharedGoals(t)
	ownerA, _, _, hhA := seedSharedGoalHousehold(t, svc, g)

	ctx := context.Background()
	ownerB := seedUser(t, g, "sg-ownerB")
	hhB, err := svc.Create(ctx, ownerB, household.CreateInput{Name: "Other"})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	cleanupHousehold(t, g, hhB.ID)
	t.Cleanup(func() {
		g.Unscoped().Where("household_id = ?", hhB.ID).Delete(&model.SavingsGoal{})
	})

	gA, err := svc.CreateSharedGoal(ctx, ownerA, hhA, household.SharedGoalInput{
		Name: "A Goal", TargetAmount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}

	// Owner B contributes to a goal that lives in household A — should
	// fail as not-a-member of A.
	_, err = svc.ContributeToSharedGoal(ctx, ownerB, hhA, gA.ID, decimal.NewFromInt(50))
	if !errors.Is(err, household.ErrNotMember) {
		t.Errorf("cross-tenant contrib err = %v, want ErrNotMember", err)
	}
	// Same goal id but scoped to household B should report not-found.
	_, err = svc.ContributeToSharedGoal(ctx, ownerB, hhB.ID, gA.ID, decimal.NewFromInt(50))
	if !errors.Is(err, household.ErrSharedGoalNotFound) {
		t.Errorf("scoped-to-wrong-household err = %v, want ErrSharedGoalNotFound", err)
	}
}
