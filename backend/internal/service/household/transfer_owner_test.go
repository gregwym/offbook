package household_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// TestTransferOwner_HappyPath: owner promotes a contributor; the promotion
// atomically demotes the old owner and promotes the new one. Ownership lives
// solely in the member role (#283).
func TestTransferOwner_HappyPath(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	ctx := context.Background()

	if err := svc.TransferOwner(ctx, ownerID, hhID, contribID); err != nil {
		t.Fatalf("TransferOwner: %v", err)
	}

	// Verify post-state via the existing Get() endpoint.
	asOldOwner, err := svc.Get(ctx, ownerID, hhID)
	if err != nil {
		t.Fatalf("Get as old owner: %v", err)
	}
	if asOldOwner.Role != model.RoleContributor {
		t.Errorf("old owner role = %q, want contributor", asOldOwner.Role)
	}
	// New owner sees themselves as owner.
	asNewOwner, err := svc.Get(ctx, contribID, hhID)
	if err != nil {
		t.Fatalf("Get as new owner: %v", err)
	}
	if asNewOwner.Role != model.RoleOwner {
		t.Errorf("new owner role = %q, want owner", asNewOwner.Role)
	}
}

// TestTransferOwner_NonOwnerForbidden: a contributor cannot trigger transfer.
func TestTransferOwner_NonOwnerForbidden(t *testing.T) {
	svc, ownerID, contribID, hhID := seedOwnerWithMember(t)
	err := svc.TransferOwner(context.Background(), contribID, hhID, ownerID)
	if !errors.Is(err, household.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

// TestTransferOwner_SelfRejected: owner can't transfer to themselves
// (no-op + would be confusing).
func TestTransferOwner_SelfRejected(t *testing.T) {
	svc, ownerID, _, hhID := seedOwnerWithMember(t)
	err := svc.TransferOwner(context.Background(), ownerID, hhID, ownerID)
	if !errors.Is(err, household.ErrCannotModifySelf) {
		t.Fatalf("err = %v, want ErrCannotModifySelf", err)
	}
}

// TestTransferOwner_TargetMustBeMember: transferring to a stranger 404s.
func TestTransferOwner_TargetMustBeMember(t *testing.T) {
	svc, ownerID, _, hhID := seedOwnerWithMember(t)
	err := svc.TransferOwner(context.Background(), ownerID, hhID, 99999)
	if !errors.Is(err, household.ErrMemberNotFound) {
		t.Fatalf("err = %v, want ErrMemberNotFound", err)
	}
}

// TestTransferOwner_RequiresDBWiring: a service constructed without
// WithDB returns ErrTxUnavailable rather than corrupting state.
func TestTransferOwner_RequiresDBWiring(t *testing.T) {
	svc, _, g := newSvc(t)
	// Strip the DB wiring that newSvc applied so we can verify the guard.
	// (newSvc already calls WithDB; this test simulates a misconfigured
	// constructor by re-creating the service without it.)
	_ = g
	ownerID := seedUser(t, g, "tx-owner")
	contribID := seedUser(t, g, "tx-contrib")
	hh, err := svc.Create(context.Background(), ownerID, household.CreateInput{Name: "TX House"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	// Override db to nil via the public WithDB API.
	svc.WithDB(nil)

	err = svc.TransferOwner(context.Background(), ownerID, hh.ID, contribID)
	if !errors.Is(err, household.ErrTxUnavailable) {
		t.Fatalf("err = %v, want ErrTxUnavailable", err)
	}
}
