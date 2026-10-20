package service_test

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
)

// newBudgetSvc returns a real BudgetService + a seeded user + a fixture
// category. The clock is fixed to 2026-05-15 12:00 UTC so period tests are
// deterministic.
func newBudgetSvc(t *testing.T) (svc *service.BudgetService, userID, categoryID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)

	suffix := time.Now().Format("150405.000000")
	cat := &model.Category{
		Name:     "BudgetFixture",
		Slug:     "budget-fixture-" + suffix,
		IsSystem: false,
	}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.Budget{})
		g.Unscoped().Delete(&model.Category{}, cat.ID)
	})

	svc = service.NewBudgetService(
		repository.NewBudgetRepository(g),
		repository.NewCategoryRepository(g),
	).WithNow(func() time.Time {
		return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	})
	return svc, userID, cat.ID, g
}

func TestBudgetService_Create_Validation(t *testing.T) {
	svc, userID, categoryID, _ := newBudgetSvc(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		in      service.CreateBudgetInput
		wantErr error
	}{
		{
			"valid monthly",
			service.CreateBudgetInput{CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(700)},
			nil,
		},
		{
			"bad period",
			service.CreateBudgetInput{CategoryID: categoryID, Period: "biweekly", Amount: decimal.NewFromInt(700)},
			service.ErrInvalidBudgetPeriod,
		},
		{
			"zero amount",
			service.CreateBudgetInput{CategoryID: categoryID, Period: "monthly", Amount: decimal.Zero},
			service.ErrInvalidBudgetAmount,
		},
		{
			"negative amount",
			service.CreateBudgetInput{CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(-1)},
			service.ErrInvalidBudgetAmount,
		},
		{
			"unknown category",
			service.CreateBudgetInput{CategoryID: 9_999_999, Period: "monthly", Amount: decimal.NewFromInt(100)},
			service.ErrUnknownCategory,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := svc.Create(ctx, repository.UserOwner(userID), tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if b == nil || b.ID == 0 {
				t.Fatalf("got %+v, want created budget", b)
			}
			t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), b.ID) })
		})
	}
}

// TestBudgetService_Create_DuplicateActiveConflicts: the partial unique
// index on (user_id, category_id, period) WHERE deleted_at IS NULL AND
// is_active = TRUE must surface as ErrDuplicateActiveBudget.
func TestBudgetService_Create_DuplicateActiveConflicts(t *testing.T) {
	svc, userID, categoryID, _ := newBudgetSvc(t)
	ctx := context.Background()

	first, err := svc.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(500),
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), first.ID) })

	_, err = svc.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(900),
	})
	if !errors.Is(err, service.ErrDuplicateActiveBudget) {
		t.Errorf("duplicate err = %v, want ErrDuplicateActiveBudget", err)
	}
}

// TestBudgetService_TenantIsolation: user B cannot read, update, or delete
// user A's budget.
func TestBudgetService_TenantIsolation(t *testing.T) {
	svc, userA, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()

	b, err := svc.Create(ctx, repository.UserOwner(userA), service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(300),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userA), b.ID) })

	userB := seedTestUser(t, g)
	if _, err := svc.Get(ctx, repository.UserOwner(userB), b.ID); !errors.Is(err, service.ErrBudgetNotFound) {
		t.Errorf("cross-tenant Get err = %v, want ErrBudgetNotFound", err)
	}
	if err := svc.SoftDelete(ctx, repository.UserOwner(userB), b.ID); !errors.Is(err, service.ErrBudgetNotFound) {
		t.Errorf("cross-tenant SoftDelete err = %v, want ErrBudgetNotFound", err)
	}
	newAmt := decimal.NewFromInt(1)
	if _, err := svc.Update(ctx, repository.UserOwner(userB), b.ID, service.UpdateBudgetInput{Amount: &newAmt}); !errors.Is(err, service.ErrBudgetNotFound) {
		t.Errorf("cross-tenant Update err = %v, want ErrBudgetNotFound", err)
	}
}

// TestBudgetService_Spend_PeriodBoundary: a transaction on the LAST day of
// April is NOT counted in May's monthly period. A May 1 transaction IS.
// Verifies the [from, to) window covers the right rows.
func TestBudgetService_Spend_PeriodBoundary(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "BudgetSpend-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})

	mkSpend := func(date time.Time, amt decimal.Decimal) {
		cat := categoryID
		tx := &model.Transaction{
			UserID:          userID,
			AccountID:       acc.ID,
			CategoryID:      &cat,
			Amount:          amt,
			TransactionDate: date,
			Source:          "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
	}
	// Clock fixed at 2026-05-15 → monthly window = [May 1, Jun 1).
	mkSpend(time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-100)) // out of period
	mkSpend(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-50))   // in period
	mkSpend(time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-25))  // in period
	mkSpend(time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(20))   // refund (#444) — nets the budget down
	mkSpend(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-999))  // out of period

	b, err := svc.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(700),
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), b.ID) })

	view, err := svc.Spend(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	want := decimal.NewFromInt(55) // 50 + 25 - 20 refund
	if !view.Spent.Equal(want) {
		t.Errorf("Spent = %s, want %s", view.Spent, want)
	}
	if !view.Limit.Equal(decimal.NewFromInt(700)) {
		t.Errorf("Limit = %s, want 700", view.Limit)
	}
	if !view.Remaining.Equal(decimal.NewFromInt(645)) {
		t.Errorf("Remaining = %s, want 645", view.Remaining)
	}
	if view.PeriodStart != (time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("PeriodStart = %v, want 2026-05-01", view.PeriodStart)
	}
	if view.PeriodEnd != (time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("PeriodEnd = %v, want 2026-06-01", view.PeriodEnd)
	}
	// 55/700 ≈ 0.079
	if view.Pct < 0.07 || view.Pct > 0.09 {
		t.Errorf("Pct = %v, want ≈0.079", view.Pct)
	}
}

// TestBudgetService_Spend_TransfersExcluded: is_transfer=true rows are NOT
// counted toward spend (internal moves shouldn't pop a budget).
func TestBudgetService_Spend_TransfersExcluded(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()

	acc := &model.Account{
		UserID: userID, Name: "BTransfer", InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	cat := categoryID
	if err := g.Create(&model.Transaction{
		UserID: userID, AccountID: acc.ID, CategoryID: &cat,
		Amount:          decimal.NewFromInt(-200),
		TransactionDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
		IsTransfer:      true,
	}).Error; err != nil {
		t.Fatalf("seed transfer: %v", err)
	}
	if err := g.Create(&model.Transaction{
		UserID: userID, AccountID: acc.ID, CategoryID: &cat,
		Amount:          decimal.NewFromInt(-30),
		TransactionDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	}).Error; err != nil {
		t.Fatalf("seed spend: %v", err)
	}

	b, err := svc.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), b.ID) })

	view, err := svc.Spend(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if !view.Spent.Equal(decimal.NewFromInt(30)) {
		t.Errorf("Spent = %s, want 30 (transfer must be excluded)", view.Spent)
	}
}

// TestBudgetService_Spend_OnlyOwnUserCounted: user B's spending in the same
// category does NOT count against user A's budget.
func TestBudgetService_Spend_OnlyOwnUserCounted(t *testing.T) {
	svc, userA, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()

	userB := seedTestUser(t, g)
	accB := &model.Account{
		UserID: userB, Name: "BAcct", InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed account B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", accB.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, accB.ID)
	})
	cat := categoryID
	if err := g.Create(&model.Transaction{
		UserID: userB, AccountID: accB.ID, CategoryID: &cat,
		Amount:          decimal.NewFromInt(-9999),
		TransactionDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	}).Error; err != nil {
		t.Fatalf("seed B txn: %v", err)
	}

	b, err := svc.Create(ctx, repository.UserOwner(userA), service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userA), b.ID) })

	view, err := svc.Spend(ctx, userA, b.ID)
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if !view.Spent.IsZero() {
		t.Errorf("Spent = %s, want 0 (user B's spend must not count)", view.Spent)
	}
}

// TestBudgetService_PeriodWindow_Weekly checks Monday-start ISO weeks.
func TestBudgetService_Spend_WeeklyMondayStart(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()
	// Clock is 2026-05-15 12:00 UTC = Friday. Weekly window = Mon May 11
	// → Mon May 18.

	acc := &model.Account{
		UserID: userID, Name: "WeeklyAcct", InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})

	cat := categoryID
	mk := func(d time.Time, amt decimal.Decimal) {
		if err := g.Create(&model.Transaction{
			UserID: userID, AccountID: acc.ID, CategoryID: &cat,
			Amount:          amt,
			TransactionDate: d, Source: "manual",
		}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mk(time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-1000)) // Sunday May 10 — prior week
	mk(time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-20))   // Monday May 11 — in week
	mk(time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-30))   // Sunday May 17 — in week
	mk(time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-9999)) // Monday May 18 — next week

	b, err := svc.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
		CategoryID: categoryID, Period: "weekly", Amount: decimal.NewFromInt(200),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, repository.UserOwner(userID), b.ID) })

	view, err := svc.Spend(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if !view.Spent.Equal(decimal.NewFromInt(50)) {
		t.Errorf("Spent = %s, want 50 (20 + 30 within Mon-Sun window)", view.Spent)
	}
}

// TestBudgetService_Spend_Rollover: unspent balance carries forward, an
// overspent period floors the balance at zero, and a non-rollover budget is
// unaffected. Clock = 2026-05-15, budget created in March.
func TestBudgetService_Spend_Rollover(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "BudgetRollover-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	mkSpend := func(date time.Time, amt int64) {
		cat := categoryID
		tx := &model.Transaction{
			UserID: userID, AccountID: acc.ID, CategoryID: &cat,
			Amount: decimal.NewFromInt(-amt), TransactionDate: date, Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
	}
	mkSpend(time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), 500) // before creation: ignored
	mkSpend(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), 40)  // Mar: +60 → 60
	mkSpend(time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), 250) // Apr: 60+100-250 <0 → 0
	mkSpend(time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), 30)   // May (current)

	owner := repository.UserOwner(userID)
	roll := true
	b, err := svc.Create(ctx, owner, service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(100), Rollover: &roll,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, owner, b.ID) })
	if err := g.Model(&model.Budget{}).Where("id = ?", b.ID).
		Update("created_at", time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatalf("backdate budget: %v", err)
	}

	view, err := svc.Spend(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	// Mar: 0+100-40=60; Apr: 60+100-250=-90 → 0; May carry = 0.
	if !view.Carryover.IsZero() {
		t.Errorf("Carryover = %s, want 0 (April overspend floors the balance)", view.Carryover)
	}
	if !view.Remaining.Equal(decimal.NewFromInt(70)) {
		t.Errorf("Remaining = %s, want 70", view.Remaining)
	}

	// Lighter April: Mar 60, Apr 60+100-20=140 → May carry 140.
	g.Unscoped().Where("account_id = ? AND transaction_date = ?", acc.ID, time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)).Delete(&model.Transaction{})
	mkSpend(time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), 20)
	view, err = svc.Spend(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if !view.Carryover.Equal(decimal.NewFromInt(140)) {
		t.Errorf("Carryover = %s, want 140", view.Carryover)
	}
	if !view.Available.Equal(decimal.NewFromInt(240)) || !view.Remaining.Equal(decimal.NewFromInt(210)) {
		t.Errorf("Available/Remaining = %s/%s, want 240/210", view.Available, view.Remaining)
	}
	if view.Pct < 0.12 || view.Pct > 0.13 { // 30/240
		t.Errorf("Pct = %v, want 0.125", view.Pct)
	}

	// Rollover off → no carryover.
	off := false
	if _, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{Rollover: &off}); err != nil {
		t.Fatalf("update: %v", err)
	}
	view, _ = svc.Spend(ctx, userID, b.ID)
	if !view.Carryover.IsZero() || !view.Remaining.Equal(decimal.NewFromInt(70)) {
		t.Errorf("rollover off: carry=%s remaining=%s, want 0/70", view.Carryover, view.Remaining)
	}
}

// TestBudgetService_Spend_RolloverStart: a starting balance seeds the
// carryover in the creation period, then folds through completed periods
// under the usual floor-at-zero policy; it is ignored while rollover is off
// and must be >= 0. Clock = 2026-05-15.
func TestBudgetService_Spend_RolloverStart(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "BudgetRolloverStart-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	cat := categoryID
	for _, s := range []struct {
		date time.Time
		amt  string
	}{
		{time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), "-40"},
		{time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), "-20.50"},
		{time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), "-30"},
	} {
		tx := &model.Transaction{
			UserID: userID, AccountID: acc.ID, CategoryID: &cat,
			Amount: decimal.RequireFromString(s.amt), TransactionDate: s.date, Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
	}

	neg := decimal.RequireFromString("-0.01")
	roll := true
	if _, err := svc.Create(ctx, owner, service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(100), Rollover: &roll, RolloverStart: &neg,
	}); !errors.Is(err, service.ErrInvalidRolloverStart) {
		t.Fatalf("negative start: err = %v, want ErrInvalidRolloverStart", err)
	}

	start := decimal.RequireFromString("25.125")
	b, err := svc.Create(ctx, owner, service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(100), Rollover: &roll, RolloverStart: &start,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	if !b.RolloverStart.Equal(start) {
		t.Fatalf("RolloverStart = %s, want %s", b.RolloverStart, start)
	}
	backdate := func(d time.Time) {
		t.Helper()
		if err := g.Model(&model.Budget{}).Where("id = ?", b.ID).Update("created_at", d).Error; err != nil {
			t.Fatalf("backdate budget: %v", err)
		}
	}

	cases := []struct {
		name       string
		created    time.Time
		rollover   bool
		wantCarry  string
		wantRemain string
	}{
		// Created this month: the starting balance is the whole carry.
		{"created current period", time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), true, "25.125", "95.125"},
		// Mar 25.125+100-40=85.125; Apr +100-20.50=164.625.
		{"folds through completed periods", time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC), true, "164.625", "234.625"},
		{"ignored while rollover off", time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC), false, "0", "70"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backdate(tc.created)
			r := tc.rollover
			if _, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{Rollover: &r}); err != nil {
				t.Fatalf("update: %v", err)
			}
			view, err := svc.Spend(ctx, userID, b.ID)
			if err != nil {
				t.Fatalf("spend: %v", err)
			}
			if !view.Carryover.Equal(decimal.RequireFromString(tc.wantCarry)) ||
				!view.Remaining.Equal(decimal.RequireFromString(tc.wantRemain)) {
				t.Errorf("carry/remaining = %s/%s, want %s/%s", view.Carryover, view.Remaining, tc.wantCarry, tc.wantRemain)
			}
		})
	}

	// Patch: negative rejected, valid value persisted.
	if _, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{RolloverStart: &neg}); !errors.Is(err, service.ErrInvalidRolloverStart) {
		t.Errorf("negative patch: err = %v, want ErrInvalidRolloverStart", err)
	}
	zero := decimal.Zero
	got, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{RolloverStart: &zero})
	if err != nil || !got.RolloverStart.IsZero() {
		t.Errorf("zero patch: start=%v err=%v", got, err)
	}
}
