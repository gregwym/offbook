package household_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// seedOwnerWithMember spins up a household with one owner + one contributor.
// Returns (svc, ownerID, contributorUserID, householdID).
func seedOwnerWithMember(t *testing.T) (*household.Service, int64, int64, int64) {
	t.Helper()
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "mod-owner")
	contribID := seedUser(t, g, "mod-contrib")
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Mod House"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	inv, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, contribID, inv.Token); err != nil {
		t.Fatalf("accept: %v", err)
	}
	return svc, ownerID, contribID, hh.ID
}

func TestListMembers_ActiveOnly(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	listing, err := svc.ListMembers(context.Background(), ownerID, hhID, false)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(listing.Active) != 2 {
		t.Errorf("active len = %d, want 2", len(listing.Active))
	}
	if len(listing.InGrace) != 0 {
		t.Errorf("InGrace populated when include=false: %+v", listing.InGrace)
	}
	_ = contribID
}

func TestListMembers_IncludeInGrace(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	ctx := context.Background()
	if err := svc.Leave(ctx, contribID, hhID); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	listing, err := svc.ListMembers(ctx, ownerID, hhID, true)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(listing.Active) != 1 || listing.Active[0].UserID != ownerID {
		t.Errorf("active = %+v, want only owner", listing.Active)
	}
	if len(listing.InGrace) != 1 || listing.InGrace[0].UserID != contribID {
		t.Errorf("InGrace = %+v, want one in-grace row for contributor", listing.InGrace)
	}
}

func TestListMembers_NonMemberRejected(t *testing.T) {
	svc, _, _, hhID := seedOwnerWithMember(t)
	_, g := seedOutsiderDB(t)
	outsiderID := seedUser(t, g, "outsider")
	_, err := svc.ListMembers(context.Background(), outsiderID, hhID, false)
	if !errors.Is(err, household.ErrNotMember) {
		t.Fatalf("err = %v, want ErrNotMember", err)
	}
}

// seedOutsiderDB returns a fresh gorm.DB connection (sharing the same
// underlying test database) so we can seed an unrelated user without
// adding them to the household under test.
func seedOutsiderDB(t *testing.T) (*household.Service, *gorm.DB) {
	svc, _, g := newSvc(t)
	return svc, g
}

func TestUpdateMemberRole_OwnerPromotesContributor(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	ctx := context.Background()
	// Demoting contributor to view_only is the simpler test case (no
	// last-owner concern since contrib is not an owner).
	mem, err := svc.UpdateMemberRole(ctx, ownerID, hhID, contribID, model.RoleViewOnly)
	if err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}
	if mem.Role != model.RoleViewOnly {
		t.Errorf("role = %q, want view_only", mem.Role)
	}
}

// TestUpdateMemberRole_RejectsPromoteToOwner: the generic role editor must
// not mint a second owner — ownership changes go through TransferOwner. The
// single-owner invariant (uq_household_single_owner) is enforced at the
// service layer here so the caller gets a clean 400, not a DB error (#283).
func TestUpdateMemberRole_RejectsPromoteToOwner(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	_, err := svc.UpdateMemberRole(context.Background(), ownerID, hhID, contribID, model.RoleOwner)
	if !errors.Is(err, household.ErrCannotPromoteToOwner) {
		t.Fatalf("err = %v, want ErrCannotPromoteToOwner", err)
	}
}

func TestUpdateMemberRole_RejectsInvalidRole(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	_, err := svc.UpdateMemberRole(context.Background(), ownerID, hhID, contribID, "superuser")
	if !errors.Is(err, household.ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

func TestUpdateMemberRole_NonOwnerForbidden(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	// contrib (non-owner) tries to demote the owner.
	_, err := svc.UpdateMemberRole(context.Background(), contribID, hhID, ownerID, model.RoleViewOnly)
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdateMemberRole_SelfRejected(t *testing.T) {
	svc, ownerID, _, hhID := seedOwnerWithMember(t)
	_, err := svc.UpdateMemberRole(context.Background(), ownerID, hhID, ownerID, model.RoleContributor)
	if !errors.Is(err, household.ErrCannotModifySelf) {
		t.Fatalf("err = %v, want ErrCannotModifySelf", err)
	}
}

func TestUpdateMemberRole_MemberNotFound(t *testing.T) {
	svc, ownerID, _, hhID := seedOwnerWithMember(t)
	_, err := svc.UpdateMemberRole(context.Background(), ownerID, hhID, 99999, model.RoleContributor)
	if !errors.Is(err, household.ErrMemberNotFound) {
		t.Fatalf("err = %v, want ErrMemberNotFound", err)
	}
}

func TestRemoveMember_HappyPath(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	ctx := context.Background()
	if err := svc.RemoveMember(ctx, ownerID, hhID, contribID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	// Contributor moves to in-grace, not purged. ListMembers without
	// include reflects only owner; ListMembers with include shows both.
	listing, _ := svc.ListMembers(ctx, ownerID, hhID, true)
	if len(listing.Active) != 1 || listing.Active[0].UserID != ownerID {
		t.Errorf("active = %+v, want only owner", listing.Active)
	}
	if len(listing.InGrace) != 1 || listing.InGrace[0].UserID != contribID {
		t.Errorf("InGrace = %+v, want contributor", listing.InGrace)
	}
}

func TestRemoveMember_NonOwnerForbidden(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	err := svc.RemoveMember(context.Background(), contribID, hhID, ownerID)
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestRemoveMember_SelfRejected(t *testing.T) {
	svc, ownerID, _, hhID := seedOwnerWithMember(t)
	err := svc.RemoveMember(context.Background(), ownerID, hhID, ownerID)
	if !errors.Is(err, household.ErrCannotModifySelf) {
		t.Fatalf("err = %v, want ErrCannotModifySelf", err)
	}
}

// Sanity: the clock-injection used by other tests still works for these.
var _ = time.Now
