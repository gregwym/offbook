// Aggregator privacy suite — covers the five scenarios from
// .claude/rules/testing.md ("Aggregator privacy tests"):
//
//	(a) private accounts excluded from aggregates
//	    → TestAggregator_ExcludesPrivateAccounts
//	(b) balance_only excluded from category-level aggregates
//	    → TestAggregator_BalanceOnlyExcludedFromCategory
//	(c) in-grace members excluded from live aggregates but present in historical
//	    → TestAggregator_InGraceExcludedFromLive       (Dashboard)
//	    → TestAggregator_InGraceExcludedFromBudgetPace (BudgetPace)
//	    → TestAggregator_HistoricalIncludesInGrace    (historical/count contract)
//	(d) return types contain no raw transaction rows (reflection check)
//	    → TestAggregator_NoRawTransactionRows
//	(e) AIContext for member A never includes member B's non-shared threads
//	    → TestAggregator_AIContextNoCrossMemberLeak
//
// The static "household package does not import pii_repo" guard lives in
// TestHouseholdPackage_DoesNotImportPIIRepo and backs ADR-0008.
package household_test

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// newAggregator wires the aggregator the same way router.go does, against
// the real test DB. Returns the aggregator + an active member helper so
// tests can avoid retyping seeding boilerplate.
func newAggregator(t *testing.T) (*household.Aggregator, *gorm.DB) {
	t.Helper()
	g := openTestDB(t)
	ensureBootstrapped(t, g)
	agg := household.NewAggregator(
		repository.NewHouseholdAggregatorRepository(g),
		repository.NewHouseholdRepository(g),
	)
	return agg, g
}

// seedHousehold creates a household with the given owner; returns the
// household and registers cleanup. Members are added separately.
func seedHouseholdRow(t *testing.T, g *gorm.DB, ownerID int64, name string, grace int) *model.Household {
	t.Helper()
	hh := &model.Household{
		Name:            name + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		OwnerID:         ownerID,
		GracePeriodDays: grace,
	}
	if err := g.Create(hh).Error; err != nil {
		t.Fatalf("seed household: %v", err)
	}
	cleanupHousehold(t, g, hh.ID)
	return hh
}

func addMember(t *testing.T, g *gorm.DB, hhID, userID int64, role string, leftAt *time.Time) *model.HouseholdMember {
	t.Helper()
	m := &model.HouseholdMember{
		HouseholdID: hhID,
		UserID:      userID,
		Role:        role,
		JoinedAt:    time.Now().Add(-30 * 24 * time.Hour),
		LeftAt:      leftAt,
	}
	if err := g.Create(m).Error; err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return m
}

func setShare(t *testing.T, g *gorm.DB, accountID, hhID int64, vis string) {
	t.Helper()
	s := &model.AccountShare{AccountID: accountID, HouseholdID: hhID, Visibility: vis}
	if err := g.Create(s).Error; err != nil {
		t.Fatalf("seed share: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.AccountShare{}, s.ID) })
}

func seedTxn(t *testing.T, g *gorm.DB, userID, accountID int64, amount string, catID *int64, when time.Time) {
	t.Helper()
	amt, _ := decimal.NewFromString(amount)
	tx := &model.Transaction{
		UserID:          userID,
		AccountID:       accountID,
		CategoryID:      catID,
		Amount:          amt,
		Currency:        "USD",
		Source:          "manual",
		TransactionDate: when,
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed txn: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
}

func setBalance(t *testing.T, g *gorm.DB, acct *model.Account, balance string) {
	t.Helper()
	d, _ := decimal.NewFromString(balance)
	if err := g.Model(acct).Update("balance", d).Error; err != nil {
		t.Fatalf("update balance: %v", err)
	}
}

// TestHouseholdPackage_DoesNotImportPIIRepo is the static guard backing
// ADR-0008 — fails if any non-test file in the household package imports
// the PII layer (pii_repo or pii_service). Test files are allowed to
// import service/ for assembling integration fixtures.
func TestHouseholdPackage_DoesNotImportPIIRepo(t *testing.T) {
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "pii") {
				t.Errorf("%s imports forbidden PII path %q — service/household must NEVER depend on PII (see ADR-0008)", name, path)
			}
		}
	}
}

// TestAggregator_ExcludesPrivateAccounts covers scenario (a).
func TestAggregator_ExcludesPrivateAccounts(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "exp-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Excludes Private", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	shared := seedAccount(t, g, ownerID, "shared")
	priv := seedAccount(t, g, ownerID, "private")
	setBalance(t, g, shared, "500.00")
	setBalance(t, g, priv, "9999.99")
	// Only the shared account has a row; the "private" one has none — that's
	// the architectural definition of private.
	setShare(t, g, shared.ID, hh.ID, model.VisibilityBalanceAndTxns)

	when := time.Now().Add(-time.Hour)
	seedTxn(t, g, ownerID, shared.ID, "-25", nil, when)
	seedTxn(t, g, ownerID, priv.ID, "-9999", nil, when) // must not surface anywhere

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if d.NetWorth != "500" {
		t.Errorf("NetWorth = %q, want 500", d.NetWorth)
	}
	if d.AccountCount != 1 {
		t.Errorf("AccountCount = %d, want 1 (private must not count)", d.AccountCount)
	}
	if d.Spending != "25" {
		t.Errorf("Spending = %q, want 25", d.Spending)
	}
	if d.TransactionCount != 1 {
		t.Errorf("TransactionCount = %d, want 1", d.TransactionCount)
	}
}

// TestAggregator_BalanceOnlyExcludedFromCategory covers scenario (b):
// balance_only contributes to net worth but never to per-category aggregates.
func TestAggregator_BalanceOnlyExcludedFromCategory(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "bo-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Balance Only", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	balOnly := seedAccount(t, g, ownerID, "bal-only")
	full := seedAccount(t, g, ownerID, "full")
	setBalance(t, g, balOnly, "1000")
	setBalance(t, g, full, "500")
	setShare(t, g, balOnly.ID, hh.ID, model.VisibilityBalanceOnly)
	setShare(t, g, full.ID, hh.ID, model.VisibilityBalanceAndTxns)

	when := time.Now().Add(-time.Hour)
	// Transaction on the balance_only account must NOT appear in the breakdown.
	seedTxn(t, g, ownerID, balOnly.ID, "-100", nil, when)
	seedTxn(t, g, ownerID, full.ID, "-50", nil, when)

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if d.NetWorth != "1500" {
		t.Errorf("NetWorth = %q, want 1500 (balance_only included)", d.NetWorth)
	}
	if d.Spending != "50" {
		t.Errorf("Spending = %q, want 50 (only balance_and_txns spend)", d.Spending)
	}
	if d.TransactionCount != 1 {
		t.Errorf("TransactionCount = %d, want 1", d.TransactionCount)
	}
	// The balance_only transaction's category must not appear at all.
	for _, row := range d.ByCategory {
		if row.Amount == "-100" {
			t.Errorf("balance_only txn leaked into ByCategory: %+v", row)
		}
	}
}

// TestAggregator_InGraceExcludedFromLive covers scenario (c) for the Dashboard
// surface: a member who left within grace must not contribute to live
// aggregates. See TestAggregator_InGraceExcludedFromBudgetPace for BudgetPace
// and TestAggregator_HistoricalIncludesInGrace for the historical/count side.
func TestAggregator_InGraceExcludedFromLive(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "grace-owner")
	leaverID := seedUser(t, g, "grace-leaver")

	hh := seedHouseholdRow(t, g, ownerID, "Grace House", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-7 * 24 * time.Hour) // left a week ago, well within 30d grace
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "owner-acct")
	leaverAcct := seedAccount(t, g, leaverID, "leaver-acct")
	setBalance(t, g, ownerAcct, "100")
	setBalance(t, g, leaverAcct, "999")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	when := time.Now().Add(-time.Hour)
	seedTxn(t, g, leaverID, leaverAcct.ID, "-555", nil, when)
	seedTxn(t, g, ownerID, ownerAcct.ID, "-10", nil, when)

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if d.NetWorth != "100" {
		t.Errorf("NetWorth = %q, want 100 (in-grace member excluded)", d.NetWorth)
	}
	if d.Spending != "10" {
		t.Errorf("Spending = %q, want 10", d.Spending)
	}
	if d.AccountCount != 1 {
		t.Errorf("AccountCount = %d, want 1", d.AccountCount)
	}
	if d.LiveMemberCount != 1 {
		t.Errorf("LiveMemberCount = %d, want 1", d.LiveMemberCount)
	}
	if d.InGraceCount != 1 {
		t.Errorf("InGraceCount = %d, want 1 (leaver still in grace window)", d.InGraceCount)
	}
}

// TestAggregator_InGraceExcludedFromBudgetPace extends scenario (c) to the
// BudgetPace surface: a shared_budget's Spent value rolls up ONLY across live
// members' transactions, so an in-grace leaver's spend in the same category
// must not bleed into the live pace.
func TestAggregator_InGraceExcludedFromBudgetPace(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "bp-owner")
	leaverID := seedUser(t, g, "bp-leaver")

	hh := seedHouseholdRow(t, g, ownerID, "BP House", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-7 * 24 * time.Hour) // well within 30d grace
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "bp-owner-acct")
	leaverAcct := seedAccount(t, g, leaverID, "bp-leaver-acct")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	cat := &model.Category{Name: "BP Groceries", Slug: fmt.Sprintf("bp-groceries-%d", time.Now().UnixNano())}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	budgetAmt, _ := decimal.NewFromString("100")
	budget := &model.SharedBudget{
		HouseholdID: hh.ID,
		CategoryID:  cat.ID,
		Period:      "monthly",
		Amount:      budgetAmt,
		IsActive:    true,
	}
	if err := g.Create(budget).Error; err != nil {
		t.Fatalf("seed shared budget: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.SharedBudget{}, budget.ID) })

	when := time.Now().Add(-time.Hour)
	seedTxn(t, g, ownerID, ownerAcct.ID, "-10", &cat.ID, when)
	seedTxn(t, g, leaverID, leaverAcct.ID, "-555", &cat.ID, when) // must not count

	items, err := agg.BudgetPace(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("BudgetPace: %v", err)
	}
	var got *household.BudgetPaceItem
	for i := range items {
		if items[i].BudgetID == budget.ID {
			got = &items[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("BudgetPace did not return our budget; got %+v", items)
	}
	if got.Spent != "10" {
		t.Errorf("Spent = %q, want 10 (leaver in grace must be excluded)", got.Spent)
	}
	if got.Pace != "0.1" {
		t.Errorf("Pace = %q, want 0.1 (10/100)", got.Pace)
	}
}

// TestAggregator_NoRawTransactionRows covers scenario (d) — reflection walk
// over every aggregator return type asserts no model.Transaction or
// []model.Transaction shapes are reachable.
func TestAggregator_NoRawTransactionRows(t *testing.T) {
	forbid := reflect.TypeOf(model.Transaction{})
	check := func(name string, sample any) {
		walkType(t, name, reflect.TypeOf(sample), forbid, map[reflect.Type]struct{}{})
	}
	check("HouseholdDashboard", household.HouseholdDashboard{})
	check("BudgetPaceItem", household.BudgetPaceItem{})
	check("GoalProgressItem", household.GoalProgressItem{})
	check("HouseholdAIContext", household.HouseholdAIContext{})
	check("ThreadSummary", household.ThreadSummary{})
}

func walkType(t *testing.T, path string, ty, forbid reflect.Type, seen map[reflect.Type]struct{}) {
	if ty == nil {
		return
	}
	if _, ok := seen[ty]; ok {
		return
	}
	seen[ty] = struct{}{}
	if ty == forbid {
		t.Errorf("%s reaches forbidden type %s — aggregator returns must not embed raw txns", path, forbid)
		return
	}
	switch ty.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan, reflect.Map:
		if ty.Kind() == reflect.Map {
			walkType(t, path+"[key]", ty.Key(), forbid, seen)
		}
		walkType(t, path+"[elem]", ty.Elem(), forbid, seen)
	case reflect.Struct:
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			walkType(t, path+"."+f.Name, f.Type, forbid, seen)
		}
	}
}

// TestAggregator_AIContextNoCrossMemberLeak covers scenario (e) — Member A's
// AIContext never includes Member B's non-shared threads.
func TestAggregator_AIContextNoCrossMemberLeak(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	a := seedUser(t, g, "ai-a")
	b := seedUser(t, g, "ai-b")
	hh := seedHouseholdRow(t, g, a, "AI House", 30)
	addMember(t, g, hh.ID, a, model.RoleOwner, nil)
	addMember(t, g, hh.ID, b, model.RoleContributor, nil)

	// Member A: one personal thread, one shared thread.
	aPersonal := &model.AIThread{UserID: a, SharedWithHousehold: false, Title: ptr("A personal")}
	if err := g.Create(aPersonal).Error; err != nil {
		t.Fatalf("seed a personal: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(aPersonal) })
	aShared := &model.AIThread{UserID: a, HouseholdID: &hh.ID, SharedWithHousehold: true, Title: ptr("A shared")}
	if err := g.Create(aShared).Error; err != nil {
		t.Fatalf("seed a shared: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(aShared) })

	// Member B: one personal thread (MUST NOT be visible to A), one shared.
	bPersonal := &model.AIThread{UserID: b, SharedWithHousehold: false, Title: ptr("B personal")}
	if err := g.Create(bPersonal).Error; err != nil {
		t.Fatalf("seed b personal: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(bPersonal) })
	bShared := &model.AIThread{UserID: b, HouseholdID: &hh.ID, SharedWithHousehold: true, Title: ptr("B shared")}
	if err := g.Create(bShared).Error; err != nil {
		t.Fatalf("seed b shared: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(bShared) })

	out, err := agg.AIContext(ctx, hh.ID, a)
	if err != nil {
		t.Fatalf("AIContext: %v", err)
	}
	for _, th := range out.PersonalThreads {
		if th.UserID != a {
			t.Errorf("AIContext.PersonalThreads leaks user %d into A's view: %+v", th.UserID, th)
		}
	}
	// Shared threads should include BOTH members' shared threads — that's the point.
	var sawA, sawB bool
	for _, th := range out.SharedThreads {
		if th.ID == aShared.ID {
			sawA = true
		}
		if th.ID == bShared.ID {
			sawB = true
		}
	}
	if !sawA || !sawB {
		t.Errorf("SharedThreads = %+v, want both A and B's shared threads", out.SharedThreads)
	}
	// And neither member's personal thread should show up in the SharedThreads list.
	for _, th := range out.SharedThreads {
		if th.ID == aPersonal.ID || th.ID == bPersonal.ID {
			t.Errorf("personal thread leaked into SharedThreads: %+v", th)
		}
	}
}

// TestAggregator_HistoricalIncludesInGrace verifies the historical lens
// (member list including left-within-grace). The current aggregator surface
// doesn't expose historical aggregates yet (deferred until shared_budgets
// roll-up history), but the partition lives in liveAndInGrace and the
// dashboard exposes the count. So we assert via the count contract.
func TestAggregator_HistoricalIncludesInGrace(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "hist-owner")
	leaverID := seedUser(t, g, "hist-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "Hist House", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	// Inside grace.
	in := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &in)

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if d.LiveMemberCount != 1 || d.InGraceCount != 1 {
		t.Errorf("counts live=%d in_grace=%d, want 1/1", d.LiveMemberCount, d.InGraceCount)
	}

	// Outside grace: pretend grace is 1 day.
	g.Model(hh).Update("grace_period_days", 1)
	d2, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard #2: %v", err)
	}
	if d2.InGraceCount != 0 {
		t.Errorf("after grace shrink, InGraceCount = %d, want 0", d2.InGraceCount)
	}
}

// TestAggregator_HouseholdNotFound surfaces a clean error when the household
// is gone (e.g. dissolved between handler call and aggregator dispatch).
func TestAggregator_HouseholdNotFound(t *testing.T) {
	agg, _ := newAggregator(t)
	if _, err := agg.Dashboard(context.Background(), 999_999_999, household.PeriodCurrentMonth); err == nil {
		t.Fatalf("expected ErrHouseholdNotFound, got nil")
	}
}

// TestAggregator_PerMemberContributions checks scenario (f) — Dashboard.Members
// gives one row per active member that owns at least one shared account,
// and each row reflects that member's contribution to net worth and
// period spending. In-grace members are excluded; balance_only accounts
// contribute to net worth only.
func TestAggregator_PerMemberContributions(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "pm-owner")
	contribID := seedUser(t, g, "pm-contrib")
	leaverID := seedUser(t, g, "pm-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "Per-Member", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	addMember(t, g, hh.ID, contribID, model.RoleContributor, nil)
	leftAt := time.Now().Add(-time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	// Owner shares a balance_and_txns checking + a balance_only savings.
	ownerChk := seedAccount(t, g, ownerID, "owner-chk")
	ownerSav := seedAccount(t, g, ownerID, "owner-sav")
	setBalance(t, g, ownerChk, "200")
	setBalance(t, g, ownerSav, "300")
	setShare(t, g, ownerChk.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, ownerSav.ID, hh.ID, model.VisibilityBalanceOnly)

	// Contributor shares one balance_and_txns account.
	contribChk := seedAccount(t, g, contribID, "contrib-chk")
	setBalance(t, g, contribChk, "100")
	setShare(t, g, contribChk.ID, hh.ID, model.VisibilityBalanceAndTxns)

	// In-grace leaver also has a shared account — must NOT appear.
	leaverChk := seedAccount(t, g, leaverID, "leaver-chk")
	setBalance(t, g, leaverChk, "9999")
	setShare(t, g, leaverChk.ID, hh.ID, model.VisibilityBalanceAndTxns)

	when := time.Now().Add(-time.Hour)
	seedTxn(t, g, ownerID, ownerChk.ID, "-40", nil, when)   // owner spend
	seedTxn(t, g, ownerID, ownerSav.ID, "-1000", nil, when) // balance_only, must be ignored
	seedTxn(t, g, contribID, contribChk.ID, "-15", nil, when)

	d, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if len(d.Members) != 2 {
		t.Fatalf("Members len = %d, want 2 (leaver excluded); got %+v", len(d.Members), d.Members)
	}
	byUser := map[int64]household.HouseholdMemberContribution{}
	for _, m := range d.Members {
		byUser[m.UserID] = m
	}
	owner := byUser[ownerID]
	if owner.NetWorthContribution != "500" {
		t.Errorf("owner NetWorthContribution = %q, want 500 (200 chk + 300 sav)", owner.NetWorthContribution)
	}
	if owner.SpendingContribution != "40" {
		t.Errorf("owner SpendingContribution = %q, want 40 (only balance_and_txns)", owner.SpendingContribution)
	}
	if owner.AccountCount != 2 {
		t.Errorf("owner AccountCount = %d, want 2 (both visibilities count toward net worth)", owner.AccountCount)
	}
	contrib := byUser[contribID]
	if contrib.NetWorthContribution != "100" {
		t.Errorf("contrib NetWorthContribution = %q, want 100", contrib.NetWorthContribution)
	}
	if contrib.SpendingContribution != "15" {
		t.Errorf("contrib SpendingContribution = %q, want 15", contrib.SpendingContribution)
	}
	if _, leakedLeaver := byUser[leaverID]; leakedLeaver {
		t.Errorf("in-grace leaver leaked into Members: %+v", d.Members)
	}
}

func ptr[T any](v T) *T { return &v }
