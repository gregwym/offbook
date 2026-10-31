package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func newGoalSvc(t *testing.T) (svc *service.SavingsGoalService, userID, accountID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "GoalFixture-" + suffix, InstitutionSlug: "fixture",
		AccountType: "savings", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.SavingsGoal{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})

	svc = service.NewSavingsGoalService(
		repository.NewSavingsGoalRepository(g),
		repository.NewAccountRepository(g),
	)
	return svc, userID, acc.ID, g
}

func TestSavingsGoalService_Create_Validation(t *testing.T) {
	svc, userID, accountID, _ := newGoalSvc(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		in      service.CreateGoalInput
		wantErr error
	}{
		{
			"valid",
			service.CreateGoalInput{Name: "Emergency", TargetAmount: decimal.NewFromInt(10000), AccountID: &accountID},
			nil,
		},
		{
			"empty name",
			service.CreateGoalInput{Name: "  ", TargetAmount: decimal.NewFromInt(100)},
			service.ErrEmptyGoalName,
		},
		{
			"zero target",
			service.CreateGoalInput{Name: "Zero", TargetAmount: decimal.Zero},
			service.ErrInvalidTargetAmount,
		},
		{
			"negative target",
			service.CreateGoalInput{Name: "Neg", TargetAmount: decimal.NewFromInt(-5)},
			service.ErrInvalidTargetAmount,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := svc.Create(ctx, repository.UserOwner(userID), tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if g == nil || g.ID == 0 {
				t.Fatalf("want created goal, got %+v", g)
			}
			t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), g.ID) })
		})
	}
}

// TestSavingsGoalService_Create_RejectsCrossUserAccount: linking to an
// account owned by another user must be rejected.
func TestSavingsGoalService_Create_RejectsCrossUserAccount(t *testing.T) {
	svc, userA, _, g := newGoalSvc(t)
	ctx := context.Background()

	userB := seedTestUser(t, g)
	accB := &model.Account{
		UserID: userB, Name: "B's", InstitutionSlug: "fixture",
		AccountType: "savings", Currency: "USD",
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed B's account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, accB.ID) })

	_, err := svc.Create(ctx, repository.UserOwner(userA), service.CreateGoalInput{
		Name: "Hack", TargetAmount: decimal.NewFromInt(100), AccountID: &accB.ID,
	})
	if !errors.Is(err, service.ErrGoalAccountMismatch) {
		t.Errorf("err = %v, want ErrGoalAccountMismatch", err)
	}
}

// TestSavingsGoalService_TenantIsolation: user B cannot Get/Update/Delete
// user A's goal, nor contribute to it.
func TestSavingsGoalService_TenantIsolation(t *testing.T) {
	svc, userA, _, g := newGoalSvc(t)
	ctx := context.Background()

	goal, err := svc.Create(ctx, repository.UserOwner(userA), service.CreateGoalInput{
		Name: "TenantIso", TargetAmount: decimal.NewFromInt(1000),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userA), goal.ID) })

	userB := seedTestUser(t, g)
	if _, err := svc.Get(ctx, repository.UserOwner(userB), goal.ID); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Get err = %v", err)
	}
	if err := svc.SoftDelete(ctx, repository.UserOwner(userB), goal.ID); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Delete err = %v", err)
	}
	if _, err := svc.Contribute(ctx, repository.UserOwner(userB), goal.ID, decimal.NewFromInt(100), userB); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Contribute err = %v", err)
	}
}

// TestSavingsGoalService_Contribute_AtomicAndPostFetched: a deposit then a
// withdrawal yields a clean net balance and the returned row reflects it.
func TestSavingsGoalService_Contribute_AtomicAndPostFetched(t *testing.T) {
	svc, userID, _, _ := newGoalSvc(t)
	ctx := context.Background()

	goal, err := svc.Create(ctx, repository.UserOwner(userID), service.CreateGoalInput{
		Name: "Atomic", TargetAmount: decimal.NewFromInt(500),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), goal.ID) })

	after, err := svc.Contribute(ctx, repository.UserOwner(userID), goal.ID, decimal.NewFromInt(200), userID)
	if err != nil {
		t.Fatalf("deposit: %v", err)
	}
	if !after.CurrentAmount.Equal(decimal.NewFromInt(200)) {
		t.Errorf("after deposit = %s, want 200", after.CurrentAmount)
	}
	after, err = svc.Contribute(ctx, repository.UserOwner(userID), goal.ID, decimal.NewFromInt(-50), userID)
	if err != nil {
		t.Fatalf("withdrawal: %v", err)
	}
	if !after.CurrentAmount.Equal(decimal.NewFromInt(150)) {
		t.Errorf("after withdrawal = %s, want 150", after.CurrentAmount)
	}
	if _, err := svc.Contribute(ctx, repository.UserOwner(userID), goal.ID, decimal.Zero, userID); !errors.Is(err, service.ErrZeroContribution) {
		t.Errorf("zero contribution err = %v", err)
	}
}

// TestSavingsGoalService_Contribute_ConcurrentSafe: 50 concurrent +1
// contributions must end at exactly 50. A naive read+write would race
// and leave the total below 50.
func TestSavingsGoalService_Contribute_ConcurrentSafe(t *testing.T) {
	svc, userID, _, _ := newGoalSvc(t)
	ctx := context.Background()

	goal, err := svc.Create(ctx, repository.UserOwner(userID), service.CreateGoalInput{
		Name: "Concurrent", TargetAmount: decimal.NewFromInt(1000),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), goal.ID) })

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Contribute(ctx, repository.UserOwner(userID), goal.ID, decimal.NewFromInt(1), userID); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent contribute err: %v", err)
	}

	g, err := svc.Get(ctx, repository.UserOwner(userID), goal.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !g.CurrentAmount.Equal(decimal.NewFromInt(n)) {
		t.Errorf("final = %s, want %d — concurrent contributions raced", g.CurrentAmount, n)
	}
}

// TestSavingsGoalService_View_ProgressAndRemaining covers the pure
// progress/remaining computation without DB round-trips.
func TestSavingsGoalService_View_ProgressAndRemaining(t *testing.T) {
	cases := []struct {
		name    string
		current string
		target  string
		wantPct float64
		wantRem string
	}{
		{"half", "50", "100", 0.5, "50"},
		{"capped at 1 when over", "150", "100", 1.0, "0"},
		{"zero current", "0", "100", 0.0, "100"},
		{"zero target", "10", "0", 0.0, "-10"}, // remaining < 0 → 0; pct = 0
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur, _ := decimal.NewFromString(tc.current)
			tgt, _ := decimal.NewFromString(tc.target)
			v := service.View(&model.SavingsGoal{CurrentAmount: cur, TargetAmount: tgt})
			if v.ProgressPct != tc.wantPct {
				t.Errorf("Pct = %v, want %v", v.ProgressPct, tc.wantPct)
			}
			// "zero target" → expected remaining clamped to 0
			wantRem := tc.wantRem
			if tc.name == "zero target" {
				wantRem = "0"
			}
			expectedRem, _ := decimal.NewFromString(wantRem)
			if !v.Remaining.Equal(expectedRem) {
				t.Errorf("Remaining = %s, want %s", v.Remaining, expectedRem)
			}
		})
	}
}

// TestSavingsGoalService_Contributions_History (#682): each contribution and
// withdrawal is recorded with its exact amount and the total after it,
// newest first; another user's goal reads as not found.
func TestSavingsGoalService_Contributions_History(t *testing.T) {
	svc, userID, _, g := newGoalSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)

	goal, err := svc.Create(ctx, owner, service.CreateGoalInput{Name: "History", TargetAmount: decimal.NewFromInt(1000)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, owner, goal.ID) })

	empty, err := svc.Contributions(ctx, owner, goal.ID)
	if err != nil {
		t.Fatalf("empty list: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("new goal history = %v, want empty non-nil slice", empty)
	}

	for _, amt := range []string{"0.000000000000000001", "250.25", "-100.5"} {
		if _, err := svc.Contribute(ctx, owner, goal.ID, decimal.RequireFromString(amt), userID); err != nil {
			t.Fatalf("contribute %s: %v", amt, err)
		}
	}
	got, err := svc.Contributions(ctx, owner, goal.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []struct{ amount, after string }{
		{"-100.5", "149.750000000000000001"},
		{"250.25", "250.250000000000000001"},
		{"0.000000000000000001", "0.000000000000000001"},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if !got[i].Amount.Equal(decimal.RequireFromString(w.amount)) {
			t.Errorf("row %d amount = %s, want %s", i, got[i].Amount, w.amount)
		}
		if !got[i].BalanceAfter.Equal(decimal.RequireFromString(w.after)) {
			t.Errorf("row %d balance_after = %s, want %s", i, got[i].BalanceAfter, w.after)
		}
		if got[i].GoalID != goal.ID {
			t.Errorf("row %d goal_id = %d, want %d", i, got[i].GoalID, goal.ID)
		}
	}

	// A rejected contribution writes no history row.
	if _, err := svc.Contribute(ctx, owner, goal.ID, decimal.Zero, userID); !errors.Is(err, service.ErrZeroContribution) {
		t.Fatalf("zero err = %v", err)
	}
	if again, _ := svc.Contributions(ctx, owner, goal.ID); len(again) != len(want) {
		t.Errorf("history grew to %d after a rejected contribution", len(again))
	}

	userB := seedTestUser(t, g)
	if _, err := svc.Contributions(ctx, repository.UserOwner(userB), goal.ID); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Contributions err = %v, want ErrSavingsGoalNotFound", err)
	}
	if _, err := svc.Contribute(ctx, repository.UserOwner(userB), goal.ID, decimal.NewFromInt(5), userB); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Contribute err = %v", err)
	}
	if after, _ := svc.Contributions(ctx, owner, goal.ID); len(after) != len(want) {
		t.Errorf("a rejected cross-tenant contribution added history (%d rows)", len(after))
	}
}

// TestSavingsGoalService_Contributions_PreHistoryTotal (#682): a goal that
// held money before history was kept still reconciles — the oldest row's
// balance_after - amount is that earlier total.
func TestSavingsGoalService_Contributions_PreHistoryTotal(t *testing.T) {
	svc, userID, _, g := newGoalSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)

	goal, err := svc.Create(ctx, owner, service.CreateGoalInput{Name: "Legacy", TargetAmount: decimal.NewFromInt(1000)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, owner, goal.ID) })
	// Simulate a pre-#682 contribution: the total moved with no history.
	if err := g.Model(&model.SavingsGoal{}).Where("id = ?", goal.ID).
		Update("current_amount", decimal.RequireFromString("300.40")).Error; err != nil {
		t.Fatalf("seed legacy total: %v", err)
	}
	if _, err := svc.Contribute(ctx, owner, goal.ID, decimal.RequireFromString("19.60"), userID); err != nil {
		t.Fatalf("contribute: %v", err)
	}
	got, err := svc.Contributions(ctx, owner, goal.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("list = %v, %v; want one row", got, err)
	}
	if !got[0].BalanceAfter.Equal(decimal.NewFromInt(320)) {
		t.Errorf("balance_after = %s, want 320", got[0].BalanceAfter)
	}
	if pre := got[0].BalanceAfter.Sub(got[0].Amount); !pre.Equal(decimal.RequireFromString("300.40")) {
		t.Errorf("pre-history total = %s, want 300.40", pre)
	}
}

// TestSavingsGoalService_Contributions_ConcurrentConsistent (#682): under
// concurrent contributions every history row's balance_after is distinct
// and the newest equals current_amount — no two writers saw the same total.
func TestSavingsGoalService_Contributions_ConcurrentConsistent(t *testing.T) {
	svc, userID, _, _ := newGoalSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)

	goal, err := svc.Create(ctx, owner, service.CreateGoalInput{Name: "Race", TargetAmount: decimal.NewFromInt(1000)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, owner, goal.ID) })

	const n = 30
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Contribute(ctx, owner, goal.ID, decimal.NewFromInt(1), userID); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent contribute: %v", err)
	}

	got, err := svc.Contributions(ctx, owner, goal.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != n {
		t.Fatalf("history rows = %d, want %d", len(got), n)
	}
	seen := map[string]bool{}
	for _, c := range got {
		k := c.BalanceAfter.String()
		if seen[k] {
			t.Errorf("two rows share balance_after %s", k)
		}
		seen[k] = true
	}
	cur, err := svc.Get(ctx, owner, goal.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got[0].BalanceAfter.Equal(cur.CurrentAmount) {
		t.Errorf("newest balance_after = %s, current_amount = %s", got[0].BalanceAfter, cur.CurrentAmount)
	}
}
