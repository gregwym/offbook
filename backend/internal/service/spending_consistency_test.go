package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestSpending_SurfacesAgreeOnSyntheticMonth is the Goal #423 gate-5 check:
// one deterministic month (200+ outflows, salary, a transfer pair, split
// transactions) must produce the same exact-decimal spend on every surface
// that reports it — headline totals, category breakdown, cash flow and the
// monthly budget actual. A drift in any one query's filters (transfer, kind,
// split handling) shows up here as a disagreement with the independently
// computed expectation.
func TestSpending_SurfacesAgreeOnSyntheticMonth(t *testing.T) {
	dash, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	cats := make([]*model.Category, 3)
	for i := range cats {
		cats[i] = &model.Category{Name: fmt.Sprintf("Consistency%d-%s", i, suffix), Slug: fmt.Sprintf("cons-%d-%s", i, suffix)}
		if err := g.Create(cats[i]).Error; err != nil {
			t.Fatalf("seed cat: %v", err)
		}
		c := cats[i]
		t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, c.ID) })
	}

	want := map[int64]decimal.Decimal{} // category id -> positive spend
	total := decimal.Zero
	var txns []*model.Transaction
	for i := 0; i < 210; i++ {
		amt := decimal.NewFromInt(int64(10 + i%37)).Add(decimal.New(int64(7+i%90), -2)).Neg() // e.g. -10.07
		cat := cats[i%3]
		tx := &model.Transaction{
			UserID: userID, AccountID: accountID, CategoryID: &cat.ID, Amount: amt,
			TransactionDate: time.Date(2026, 5, 1+i%28, 0, 0, 0, 0, time.UTC), Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn %d: %v", i, err)
		}
		txns = append(txns, tx)
		want[cat.ID] = want[cat.ID].Add(amt.Neg())
		total = total.Add(amt.Neg())
	}

	// Salary inflow and a transfer pair must not appear as spending.
	salary := decimal.RequireFromString("3200.00")
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), salary, false)
	seedChartTxn(t, g, userID, accountID, &cats[0].ID, time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-500), true)
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(500), true)

	// Split three transactions across categories; the parent total is
	// unchanged but the allocation moves between categories.
	txRepo := repository.NewTransactionRepository(g)
	for _, idx := range []int{0, 1, 2} {
		p := txns[idx]
		first := p.Amount.Div(decimal.NewFromInt(3)).Round(2) // exact 2dp part
		rest := p.Amount.Sub(first)
		from := *p.CategoryID
		to := cats[(idx+1)%3].ID
		lines := []model.TransactionSplit{
			{CategoryID: &from, Amount: first},
			{CategoryID: &to, Amount: rest},
		}
		if err := txRepo.ReplaceSplits(ctx, userID, p.ID, lines); err != nil {
			t.Fatalf("split %d: %v", idx, err)
		}
		want[from] = want[from].Sub(p.Amount.Neg()).Add(first.Neg())
		want[to] = want[to].Add(rest.Neg())
	}

	sumWant := decimal.Zero
	for _, v := range want {
		sumWant = sumWant.Add(v)
	}
	if !sumWant.Equal(total) {
		t.Fatalf("fixture bug: allocations %s != total %s", sumWant, total)
	}

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	eq := func(t *testing.T, what, got string, exp decimal.Decimal) {
		t.Helper()
		v, err := decimal.NewFromString(got)
		if err != nil || !v.Equal(exp) {
			t.Errorf("%s = %q, want %s", what, got, exp)
		}
	}

	t.Run("headline totals", func(t *testing.T) {
		sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "spending", sum.Spending, total)
		eq(t, "income", sum.Income, salary)
		for _, row := range sum.ByCategory {
			if row.CategoryID == nil {
				continue
			}
			if w, ok := want[*row.CategoryID]; ok {
				eq(t, "summary by_category "+row.Name, row.Amount, w.Neg())
			}
		}
	})

	t.Run("category breakdown", func(t *testing.T) {
		items, err := dash.SpendByCategory(ctx, userID, from, to)
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, it := range items {
			if it.CategoryID == nil {
				continue
			}
			if w, ok := want[*it.CategoryID]; ok {
				seen++
				eq(t, "spend_by_category "+it.Name, it.Amount, w)
			}
		}
		if seen != len(want) {
			t.Errorf("breakdown covered %d of %d categories", seen, len(want))
		}
	})

	t.Run("cash flow", func(t *testing.T) {
		months, err := dash.CashFlow(ctx, userID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(months) != 1 {
			t.Fatalf("got %d months, want 1", len(months))
		}
		eq(t, "outflow", months[0].Outflow, total)
		eq(t, "inflow", months[0].Inflow, salary)
		eq(t, "net", months[0].Net, salary.Sub(total))
	})

	t.Run("budget actual", func(t *testing.T) {
		budgets := service.NewBudgetService(
			repository.NewBudgetRepository(g),
			repository.NewCategoryRepository(g),
		).WithNow(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
		t.Cleanup(func() { g.Unscoped().Where("user_id = ?", userID).Delete(&model.Budget{}) })
		for _, c := range cats {
			b, err := budgets.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
				CategoryID: c.ID, Period: "monthly", Amount: decimal.NewFromInt(5000),
			})
			if err != nil {
				t.Fatalf("create budget: %v", err)
			}
			sp, err := budgets.Spend(ctx, userID, b.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !sp.Spent.Equal(want[c.ID]) {
				t.Errorf("budget spent for %s = %s, want %s", c.Name, sp.Spent, want[c.ID])
			}
			if !sp.Remaining.Equal(decimal.NewFromInt(5000).Sub(want[c.ID])) {
				t.Errorf("budget remaining for %s = %s", c.Name, sp.Remaining)
			}
		}
	})
}

// TestSpending_SurfacesAgreeOnRefundsAndHiddenRows extends the gate-5 check to
// the rest of the acceptance fixture: a refund must net against its category's
// spend (and not count as income), a hidden row and a credit-card payment pair
// must not count at all, and every surface must report the same exact figure.
func TestSpending_SurfacesAgreeOnRefundsAndHiddenRows(t *testing.T) {
	dash, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	groceries := &model.Category{Name: "RefundG-" + suffix, Slug: "refund-g-" + suffix}
	dining := &model.Category{Name: "RefundD-" + suffix, Slug: "refund-d-" + suffix}
	for _, c := range []*model.Category{groceries, dining} {
		if err := g.Create(c).Error; err != nil {
			t.Fatalf("seed cat: %v", err)
		}
		c := c
		t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, c.ID) })
	}
	day := func(d int) time.Time { return time.Date(2026, 5, d, 0, 0, 0, 0, time.UTC) }
	dec := decimal.RequireFromString

	seedChartTxn(t, g, userID, accountID, &groceries.ID, day(3), dec("-120.35"), false)
	seedChartTxn(t, g, userID, accountID, &groceries.ID, day(9), dec("-44.65"), false)
	seedChartTxn(t, g, userID, accountID, &groceries.ID, day(12), dec("25.10"), false) // refund
	seedChartTxn(t, g, userID, accountID, &dining.ID, day(5), dec("-60.00"), false)
	seedChartTxn(t, g, userID, accountID, nil, day(1), dec("2000.00"), false) // salary
	// Credit-card payment: transfer pair, never spending.
	seedChartTxn(t, g, userID, accountID, nil, day(20), dec("-900.00"), true)
	seedChartTxn(t, g, userID, accountID, nil, day(20), dec("900.00"), true)
	// Hidden row: kept in the ledger, excluded from spending.
	hiddenAt := day(21)
	hidden := &model.Transaction{
		UserID: userID, AccountID: accountID, CategoryID: &dining.ID, Amount: dec("-77.00"),
		TransactionDate: day(21), Source: "manual", HiddenAt: &hiddenAt,
	}
	if err := g.Create(hidden).Error; err != nil {
		t.Fatalf("seed hidden: %v", err)
	}

	wantG := dec("139.90") // 120.35 + 44.65 - 25.10
	wantD := dec("60.00")
	wantTotal := wantG.Add(wantD)
	salary := dec("2000.00")

	eq := func(t *testing.T, what, got string, exp decimal.Decimal) {
		t.Helper()
		v, err := decimal.NewFromString(got)
		if err != nil || !v.Equal(exp) {
			t.Errorf("%s = %q, want %s", what, got, exp)
		}
	}

	t.Run("headline totals", func(t *testing.T) {
		sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "spending", sum.Spending, wantTotal)
		eq(t, "income", sum.Income, salary)
	})
	t.Run("category breakdown", func(t *testing.T) {
		items, err := dash.SpendByCategory(ctx, userID, day(1), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if it.CategoryID == nil {
				continue
			}
			switch *it.CategoryID {
			case groceries.ID:
				eq(t, "groceries", it.Amount, wantG)
			case dining.ID:
				eq(t, "dining", it.Amount, wantD)
			}
		}
	})
	t.Run("cash flow", func(t *testing.T) {
		months, err := dash.CashFlow(ctx, userID, 1)
		if err != nil || len(months) != 1 {
			t.Fatalf("months=%v err=%v", months, err)
		}
		eq(t, "outflow", months[0].Outflow, wantTotal)
		eq(t, "inflow", months[0].Inflow, salary)
	})
	t.Run("top merchants", func(t *testing.T) {
		// Every seeded row has no merchant label, so they all group under
		// "Unknown": the refund nets in and the hidden row and card payment
		// stay out, exactly as on the other surfaces (#532).
		items, err := dash.TopMerchants(ctx, userID, day(1), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 10)
		if err != nil || len(items) != 1 {
			t.Fatalf("items=%+v err=%v", items, err)
		}
		eq(t, "merchant total", items[0].Amount, wantTotal)
	})
	t.Run("budget actual", func(t *testing.T) {
		budgets := service.NewBudgetService(
			repository.NewBudgetRepository(g),
			repository.NewCategoryRepository(g),
		).WithNow(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
		t.Cleanup(func() { g.Unscoped().Where("user_id = ?", userID).Delete(&model.Budget{}) })
		for c, w := range map[*model.Category]decimal.Decimal{groceries: wantG, dining: wantD} {
			b, err := budgets.Create(ctx, repository.UserOwner(userID), service.CreateBudgetInput{
				CategoryID: c.ID, Period: "monthly", Amount: decimal.NewFromInt(1000),
			})
			if err != nil {
				t.Fatalf("create budget: %v", err)
			}
			sp, err := budgets.Spend(ctx, userID, b.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !sp.Spent.Equal(w) {
				t.Errorf("budget spent for %s = %s, want %s", c.Name, sp.Spent, w)
			}
		}
	})
}
