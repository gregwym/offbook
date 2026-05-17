package household_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

const testSecret = "household-test-secret"

func loadRepoDotenv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	loadRepoDotenv()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no DATABASE_URL set; skipping integration test")
	}
	g, err := db.Open(url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx, g); err != nil {
		t.Skipf("db.Ping: %v; skipping integration test", err)
	}
	return g
}

// newSvc builds a real household.Service backed by Postgres, and pre-seeds
// instance_config so invite creation is not gated by ErrInstanceNotReady.
// All seeded rows are cleaned up via t.Cleanup.
func newSvc(t *testing.T) (*household.Service, *service.AccountService, *gorm.DB) {
	t.Helper()
	g := openTestDB(t)
	ensureBootstrapped(t, g)
	accRepo := repository.NewAccountRepository(g)
	svc := household.NewService(
		repository.NewHouseholdRepository(g),
		repository.NewHouseholdMemberRepository(g),
		repository.NewHouseholdInviteRepository(g),
		repository.NewAccountShareRepository(g),
		accRepo,
		repository.NewInstanceConfigRepository(g),
		repository.NewUserRepository(g),
		testSecret,
	)
	return svc, service.NewAccountService(accRepo), g
}

// ensureBootstrapped writes a singleton instance_config row if missing so the
// household service's CreateInvite passes its "not bootstrapped" defense.
// We don't clean this up because it's harmless idempotent state shared across tests.
func ensureBootstrapped(t *testing.T, g *gorm.DB) {
	t.Helper()
	var cfg model.InstanceConfig
	if err := g.First(&cfg).Error; err == nil {
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("read instance_config: %v", err)
	}
	cfg = model.InstanceConfig{ID: 1, SignupMode: model.SignupModeInviteOnly}
	if err := g.Create(&cfg).Error; err != nil {
		t.Fatalf("seed instance_config: %v", err)
	}
}

// seedUser inserts a throwaway user and registers a cascading cleanup that
// strips FK-dependent rows (shares, members, accounts, sessions) before the
// user itself is removed. This makes tests independent of t.Cleanup ordering.
func seedUser(t *testing.T, g *gorm.DB, label string) int64 {
	t.Helper()
	u := &model.User{
		Email:        fmt.Sprintf("hh-%s-%d@example.test", label, time.Now().UnixNano()),
		PasswordHash: "x",
		LastScope:    model.ScopePersonal,
		DefaultScope: model.ScopePersonal,
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		var accountIDs []int64
		g.Unscoped().Model(&model.Account{}).Where("user_id = ?", u.ID).Pluck("id", &accountIDs)
		if len(accountIDs) > 0 {
			g.Unscoped().Where("account_id IN ?", accountIDs).Delete(&model.AccountShare{})
		}
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Account{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.HouseholdMember{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Session{})
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	return u.ID
}

func seedAccount(t *testing.T, g *gorm.DB, userID int64, label string) *model.Account {
	t.Helper()
	acc := &model.Account{
		UserID:          userID,
		Name:            "hh-acct-" + label + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		InstitutionSlug: "fixture",
		AccountType:     "checking",
		Currency:        "USD",
		Balance:         decimal.Zero,
		IsActive:        true,
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, acc.ID) })
	return acc
}

// cleanupHousehold removes the household and all its dependent rows. The model
// uses partial unique indexes (purged_at, deleted_at), and tests share a DB,
// so we hard-delete by IDs to keep tests independent.
func cleanupHousehold(t *testing.T, g *gorm.DB, householdID int64) {
	t.Helper()
	t.Cleanup(func() {
		g.Unscoped().Where("household_id = ?", householdID).Delete(&model.AccountShare{})
		g.Unscoped().Where("household_id = ?", householdID).Delete(&model.HouseholdInvite{})
		g.Unscoped().Where("household_id = ?", householdID).Delete(&model.HouseholdMember{})
		g.Unscoped().Delete(&model.Household{}, householdID)
	})
}

// TestCreate_CreatorBecomesOwner verifies the creator is enrolled as `owner`
// and the household carries the chosen grace_period_days.
func TestCreate_CreatorBecomesOwner(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	userID := seedUser(t, g, "creator")

	grace := 14
	hh, err := svc.Create(ctx, userID, household.CreateInput{
		Name:            "Test House",
		GracePeriodDays: &grace,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	if hh.OwnerID != userID {
		t.Errorf("OwnerID = %d, want %d", hh.OwnerID, userID)
	}
	if hh.GracePeriodDays != grace {
		t.Errorf("GracePeriodDays = %d, want %d", hh.GracePeriodDays, grace)
	}
	detail, err := svc.Get(ctx, userID, hh.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Role != model.RoleOwner {
		t.Errorf("requester role = %q, want owner", detail.Role)
	}
	if len(detail.Members) != 1 || detail.Members[0].UserID != userID || detail.Members[0].Role != model.RoleOwner {
		t.Errorf("members = %+v, want exactly one owner row for caller", detail.Members)
	}
}

// TestCreate_RejectsSecondHouseholdForUser enforces at-most-one (ADR-0006).
func TestCreate_RejectsSecondHouseholdForUser(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	userID := seedUser(t, g, "double")

	hh1, err := svc.Create(ctx, userID, household.CreateInput{Name: "First"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	cleanupHousehold(t, g, hh1.ID)

	_, err = svc.Create(ctx, userID, household.CreateInput{Name: "Second"})
	if !errors.Is(err, household.ErrAlreadyMember) {
		t.Errorf("second create err = %v, want ErrAlreadyMember", err)
	}
}

// TestInviteAndAccept_HappyPath covers the canonical invite flow: owner mints
// a token, the invitee accepts, both are active members.
func TestInviteAndAccept_HappyPath(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "owner")
	guestID := seedUser(t, g, "guest")

	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Invite House"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	res, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if res.Token == "" {
		t.Fatalf("expected raw token in CreateInviteResult")
	}

	accepted, err := svc.AcceptInvite(ctx, guestID, res.Token)
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	if accepted.Resumed {
		t.Errorf("Resumed = true, want false on fresh accept")
	}
	if accepted.Member.UserID != guestID || accepted.Member.Role != model.RoleContributor {
		t.Errorf("Member = %+v, want guest as contributor", accepted.Member)
	}

	detail, err := svc.Get(ctx, ownerID, hh.ID)
	if err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if len(detail.Members) != 2 {
		t.Errorf("members count = %d, want 2", len(detail.Members))
	}

	// Re-accepting the same token by a different user must be rejected.
	other := seedUser(t, g, "other")
	if _, err := svc.AcceptInvite(ctx, other, res.Token); !errors.Is(err, household.ErrInviteAlreadyAccepted) {
		t.Errorf("re-accept by another user err = %v, want ErrInviteAlreadyAccepted", err)
	}
}

// TestAccept_InviteExpired exercises the InviteTTL boundary.
func TestAccept_InviteExpired(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "expire-owner")
	guestID := seedUser(t, g, "expire-guest")

	// Freeze clock far in past for invite minting.
	past := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return past })
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Expiring"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	res, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	// Jump past TTL.
	svc.SetClock(func() time.Time { return past.Add(household.InviteTTL + time.Hour) })
	if _, err := svc.AcceptInvite(ctx, guestID, res.Token); !errors.Is(err, household.ErrInviteExpired) {
		t.Errorf("err = %v, want ErrInviteExpired", err)
	}
}

// TestLeaveAndRejoin_PreservesShares is the headline ADR-0007 case: a member
// leaves (left_at set), the share they had on their account stays put, and
// re-accepting a fresh invite within grace clears left_at and they see the
// same account_shares row.
func TestLeaveAndRejoin_PreservesShares(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "rj-owner")
	guestID := seedUser(t, g, "rj-guest")

	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Rejoin House"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	// Invite + accept guest.
	inv, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, guestID, inv.Token); err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}

	// Guest shares one of their own accounts with the household.
	guestAcct := seedAccount(t, g, guestID, "shared")
	share, err := svc.SetShare(ctx, guestID, guestAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	if err != nil {
		t.Fatalf("SetShare: %v", err)
	}
	if share == nil || share.Visibility != model.VisibilityBalanceAndTxns {
		t.Fatalf("share = %+v, want balance_and_txns", share)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.AccountShare{}, share.ID) })

	// Guest leaves.
	if err := svc.Leave(ctx, guestID, hh.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	// Verify left_at is set, share row is still intact.
	var memAfter model.HouseholdMember
	if err := g.Where("household_id = ? AND user_id = ?", hh.ID, guestID).First(&memAfter).Error; err != nil {
		t.Fatalf("re-read member: %v", err)
	}
	if memAfter.LeftAt == nil {
		t.Errorf("LeftAt = nil after Leave, want set")
	}
	var shareAfter model.AccountShare
	if err := g.First(&shareAfter, share.ID).Error; err != nil {
		t.Errorf("account_shares row purged on leave: %v (must persist for rejoin per ADR-0007)", err)
	}

	// Owner issues a new invite; guest accepts; auto-resume clears left_at.
	inv2, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{})
	if err != nil {
		t.Fatalf("second CreateInvite: %v", err)
	}
	res, err := svc.AcceptInvite(ctx, guestID, inv2.Token)
	if err != nil {
		t.Fatalf("rejoin AcceptInvite: %v", err)
	}
	if !res.Resumed {
		t.Errorf("Resumed = false, want true on auto-resume")
	}

	// left_at must be cleared and the SAME member row reused (no duplicate).
	var memRejoined model.HouseholdMember
	if err := g.First(&memRejoined, memAfter.ID).Error; err != nil {
		t.Fatalf("re-read member after rejoin: %v", err)
	}
	if memRejoined.LeftAt != nil {
		t.Errorf("LeftAt = %v after rejoin, want nil", memRejoined.LeftAt)
	}
	var dupCount int64
	g.Model(&model.HouseholdMember{}).
		Where("household_id = ? AND user_id = ? AND purged_at IS NULL", hh.ID, guestID).
		Count(&dupCount)
	if dupCount != 1 {
		t.Errorf("active membership rows for guest = %d, want exactly 1", dupCount)
	}
}

// TestLeave_LastOwnerReturns409 verifies the canonical ADR-0007 guard.
func TestLeave_LastOwnerReturns409(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "lo")

	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Solo"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	if err := svc.Leave(ctx, ownerID, hh.ID); !errors.Is(err, household.ErrLastOwner) {
		t.Errorf("err = %v, want ErrLastOwner", err)
	}

	// Owner with a contributor present — leaving still rejected because the
	// contributor isn't an owner.
	contribID := seedUser(t, g, "lo-contrib")
	inv, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, contribID, inv.Token); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := svc.Leave(ctx, ownerID, hh.ID); !errors.Is(err, household.ErrLastOwner) {
		t.Errorf("with contributor, owner leave err = %v, want ErrLastOwner", err)
	}
}

// TestSetShare_RoundTripsAllVisibilityStates covers the API contract:
// each PUT (balance_only → balance_and_txns → private) is observable on
// the next GET; private deletes the row.
func TestSetShare_RoundTripsAllVisibilityStates(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "share-owner")
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Share House"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	acct := seedAccount(t, g, ownerID, "share-flow")

	// Initial state: no share row exists for this account.
	shares, err := svc.ListShares(ctx, ownerID, acct.ID)
	if err != nil {
		t.Fatalf("ListShares: %v", err)
	}
	if len(shares) != 0 {
		t.Errorf("initial shares = %d, want 0", len(shares))
	}

	cases := []struct {
		name       string
		visibility string
		expectRow  bool
	}{
		{"balance_only first", model.VisibilityBalanceOnly, true},
		{"upgrade to balance_and_txns", model.VisibilityBalanceAndTxns, true},
		{"private deletes", model.VisibilityPrivate, false},
		{"re-share after delete", model.VisibilityBalanceOnly, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			share, err := svc.SetShare(ctx, ownerID, acct.ID, hh.ID, tc.visibility)
			if err != nil {
				t.Fatalf("SetShare %q: %v", tc.visibility, err)
			}
			got, err := svc.ListShares(ctx, ownerID, acct.ID)
			if err != nil {
				t.Fatalf("ListShares: %v", err)
			}
			if tc.expectRow {
				if share == nil || share.Visibility != tc.visibility {
					t.Errorf("returned share = %+v, want visibility %q", share, tc.visibility)
				}
				if len(got) != 1 || got[0].Visibility != tc.visibility {
					t.Errorf("ListShares = %+v, want one row with %q", got, tc.visibility)
				}
			} else {
				if share != nil {
					t.Errorf("SetShare(private) returned %+v, want nil", share)
				}
				if len(got) != 0 {
					t.Errorf("ListShares after private = %d rows, want 0", len(got))
				}
			}
		})
	}
}

// TestSetShare_RejectsInvalidVisibility exercises the validation contract.
func TestSetShare_RejectsInvalidVisibility(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "share-bad")
	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Bad Vis"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	acct := seedAccount(t, g, ownerID, "bad-vis")

	if _, err := svc.SetShare(ctx, ownerID, acct.ID, hh.ID, "bogus"); !errors.Is(err, household.ErrInvalidVisibility) {
		t.Errorf("err = %v, want ErrInvalidVisibility", err)
	}
}

// TestShares_TenantIsolation is the multi-tenant guard for the share endpoints.
// User B must not be able to read or write the shares of User A's account.
func TestShares_TenantIsolation(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	userA := seedUser(t, g, "iso-a")
	userB := seedUser(t, g, "iso-b")

	hh, err := svc.Create(ctx, userA, household.CreateInput{Name: "Iso"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	acctA := seedAccount(t, g, userA, "iso-a-acct")

	if _, err := svc.ListShares(ctx, userB, acctA.ID); !errors.Is(err, household.ErrAccountNotOwned) {
		t.Errorf("B list shares of A's account err = %v, want ErrAccountNotOwned", err)
	}
	if _, err := svc.SetShare(ctx, userB, acctA.ID, hh.ID, model.VisibilityBalanceOnly); !errors.Is(err, household.ErrAccountNotOwned) {
		t.Errorf("B set share of A's account err = %v, want ErrAccountNotOwned", err)
	}
}

// TestGet_NonMemberRejected verifies non-members can't fingerprint household
// existence via the detail endpoint.
func TestGet_NonMemberRejected(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "g-owner")
	outsiderID := seedUser(t, g, "g-outsider")

	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Closed"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)

	if _, err := svc.Get(ctx, outsiderID, hh.ID); !errors.Is(err, household.ErrNotMember) {
		t.Errorf("non-member Get err = %v, want ErrNotMember", err)
	}
}

// TestUpdate_OwnerOnly enforces that contributors cannot mutate household state.
func TestUpdate_OwnerOnly(t *testing.T) {
	svc, _, g := newSvc(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "u-owner")
	contribID := seedUser(t, g, "u-contrib")

	hh, err := svc.Create(ctx, ownerID, household.CreateInput{Name: "Owner Only"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	inv, err := svc.CreateInvite(ctx, ownerID, hh.ID, household.CreateInviteInput{Role: model.RoleContributor})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := svc.AcceptInvite(ctx, contribID, inv.Token); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	newName := "Renamed By Contrib"
	if _, err := svc.Update(ctx, contribID, hh.ID, household.UpdateInput{Name: &newName}); !errors.Is(err, household.ErrForbidden) {
		t.Errorf("contributor Update err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Update(ctx, ownerID, hh.ID, household.UpdateInput{Name: &newName}); err != nil {
		t.Errorf("owner Update err = %v, want nil", err)
	}
}
