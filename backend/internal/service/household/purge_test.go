package household_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// TestRunPurge_SealsExpiredAndDeletesShares: an in-grace member whose
// grace expired is sealed (purged_at set) and their account_shares row
// in the household is gone. A second in-grace member still within grace
// stays untouched.
func TestRunPurge_SealsExpiredAndDeletesShares(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "purge-owner")
	expiredID := seedUser(t, g, "purge-expired")
	fresherID := seedUser(t, g, "purge-fresher")

	// 30-day grace by default; pin time so the expired member is 60 days
	// past grace and the fresher one is still inside.
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Purge House"})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	// Add both members via accepted invites, then mark each "left" at a
	// known date.
	inv1, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("invite expired: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, expiredID, inv1.Token); err != nil {
		t.Fatalf("accept expired: %v", err)
	}
	inv2, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("invite fresher: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, fresherID, inv2.Token); err != nil {
		t.Fatalf("accept fresher: %v", err)
	}

	// Each member shares an account with the household — those rows are
	// what the runner needs to delete for the expired user.
	expAcct := seedAcct(t, g, expiredID)
	freshAcct := seedAcct(t, g, fresherID)
	if _, err := svc.SetShare(ctx, expiredID, expAcct.ID, hh.ID, model.VisibilityBalanceAndTxns); err != nil {
		t.Fatalf("share expired: %v", err)
	}
	if _, err := svc.SetShare(ctx, fresherID, freshAcct.ID, hh.ID, model.VisibilityBalanceAndTxns); err != nil {
		t.Fatalf("share fresher: %v", err)
	}

	// Force the left_at timestamps. The household's default grace is 30d.
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	expiredLeft := now.Add(-60 * 24 * time.Hour) // 30 past grace
	fresherLeft := now.Add(-5 * 24 * time.Hour)  // still inside grace
	if err := g.Model(&model.HouseholdMember{}).
		Where("household_id = ? AND user_id = ?", hh.ID, expiredID).
		Update("left_at", expiredLeft).Error; err != nil {
		t.Fatalf("set expired left_at: %v", err)
	}
	if err := g.Model(&model.HouseholdMember{}).
		Where("household_id = ? AND user_id = ?", hh.ID, fresherID).
		Update("left_at", fresherLeft).Error; err != nil {
		t.Fatalf("set fresher left_at: %v", err)
	}

	res, err := household.RunPurge(ctx, g, now)
	if err != nil {
		t.Fatalf("RunPurge: %v", err)
	}
	if res.MembersPurged != 1 {
		t.Errorf("MembersPurged = %d, want 1", res.MembersPurged)
	}
	if res.SharesDeleted != 1 {
		t.Errorf("SharesDeleted = %d, want 1", res.SharesDeleted)
	}

	// Verify expired user's membership row has purged_at set; fresher's doesn't.
	var expMember, freshMember model.HouseholdMember
	if err := g.Where("household_id = ? AND user_id = ?", hh.ID, expiredID).
		First(&expMember).Error; err != nil {
		t.Fatalf("load expired member: %v", err)
	}
	if expMember.PurgedAt == nil {
		t.Errorf("expired member purged_at = nil, want set")
	}
	if err := g.Where("household_id = ? AND user_id = ?", hh.ID, fresherID).
		First(&freshMember).Error; err != nil {
		t.Fatalf("load fresher member: %v", err)
	}
	if freshMember.PurgedAt != nil {
		t.Errorf("fresher member purged_at = %v, want nil (still in grace)", freshMember.PurgedAt)
	}

	// Verify account_shares: expired's share is gone, fresher's remains.
	var expShareCount, freshShareCount int64
	if err := g.Raw(`
		SELECT COUNT(*) FROM account_shares
		WHERE household_id = ? AND account_id = ? AND deleted_at IS NULL
	`, hh.ID, expAcct.ID).Scan(&expShareCount).Error; err != nil {
		t.Fatalf("count expired shares: %v", err)
	}
	if expShareCount != 0 {
		t.Errorf("expired account_shares count = %d, want 0", expShareCount)
	}
	if err := g.Raw(`
		SELECT COUNT(*) FROM account_shares
		WHERE household_id = ? AND account_id = ? AND deleted_at IS NULL
	`, hh.ID, freshAcct.ID).Scan(&freshShareCount).Error; err != nil {
		t.Fatalf("count fresher shares: %v", err)
	}
	if freshShareCount != 1 {
		t.Errorf("fresher account_shares count = %d, want 1", freshShareCount)
	}
}

// TestRunPurge_Idempotent: re-running on a clean DB is a no-op.
func TestRunPurge_Idempotent(t *testing.T) {
	_, _, g := newSvc(t)
	res, err := household.RunPurge(context.Background(), g, time.Now())
	if err != nil {
		t.Fatalf("first RunPurge: %v", err)
	}
	res2, err := household.RunPurge(context.Background(), g, time.Now())
	if err != nil {
		t.Fatalf("second RunPurge: %v", err)
	}
	if res2.MembersPurged != res.MembersPurged-res.MembersPurged {
		// Either zero on first or zero on second — what matters is that
		// re-running doesn't double-process. The arithmetic above forces
		// `res2.MembersPurged == 0` regardless of what the first call
		// did (other tests may have left in-grace rows on this DB).
		t.Errorf("second RunPurge purged %d more, want 0", res2.MembersPurged)
	}
}

// TestRunPurge_RespectsHouseholdGracePeriod: a household with a shorter
// grace period purges sooner than the 30-day default.
func TestRunPurge_RespectsHouseholdGracePeriod(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "grace-owner")
	leaverID := seedUser(t, g, "grace-leaver")

	// 7-day grace.
	graceDays := 7
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{
		Name:            "Tight Grace",
		GracePeriodDays: &graceDays,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	inv, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, leaverID, inv.Token); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// 10 days past leave > 7-day grace.
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	leftAt := now.Add(-10 * 24 * time.Hour)
	if err := g.Model(&model.HouseholdMember{}).
		Where("household_id = ? AND user_id = ?", hh.ID, leaverID).
		Update("left_at", leftAt).Error; err != nil {
		t.Fatalf("set left_at: %v", err)
	}

	res, err := household.RunPurge(ctx, g, now)
	if err != nil {
		t.Fatalf("RunPurge: %v", err)
	}
	if res.MembersPurged != 1 {
		t.Errorf("MembersPurged = %d, want 1 (7-day grace expired)", res.MembersPurged)
	}
}

// seedAcct creates a fresh investment-style account owned by the user
// and registers cleanup. Used for the share-deletion assertions.
func seedAcct(t *testing.T, g *gorm.DB, userID int64) *model.Account {
	t.Helper()
	a := &model.Account{
		UserID:          userID,
		Name:            "purge-acct-" + time.Now().Format("150405.000000000"),
		InstitutionSlug: "fixture",
		AccountType:     "checking",
		Currency:        "USD",
	}
	if err := g.Create(a).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", a.ID).Delete(&model.AccountShare{})
		g.Unscoped().Delete(&model.Account{}, a.ID)
	})
	return a
}
