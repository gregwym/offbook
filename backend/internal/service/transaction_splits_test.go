package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestTransactionService_ReplaceSplits(t *testing.T) {
	svc, userID, accountID, catA, g := newTxSvc(t)
	ctx := context.Background()
	catB := &model.Category{Name: "SplitB", Slug: "split-b-" + time.Now().Format("150405.000000")}
	if err := g.Create(catB).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, catB.ID) })

	in := validTxInput(accountID)
	in.Amount = d("-100.10")
	in.TransactionDate = time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	tx, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, tx.ID) })
	a, b := catA, catB.ID

	t.Run("rejects invalid sets", func(t *testing.T) {
		cases := map[string][]service.SplitInput{
			"single line":  {{CategoryID: &a, Amount: d("-100.10")}},
			"sum mismatch": {{CategoryID: &a, Amount: d("-60.00")}, {CategoryID: &b, Amount: d("-40.09")}},
			"wrong sign":   {{CategoryID: &a, Amount: d("200.20")}, {CategoryID: &b, Amount: d("-100.10")}},
			"zero line":    {{CategoryID: &a, Amount: d("-100.10")}, {CategoryID: &b, Amount: d("0")}},
		}
		for name, lines := range cases {
			if _, err := svc.ReplaceSplits(ctx, userID, tx.ID, lines); !errors.Is(err, service.ErrInvalidSplits) {
				t.Errorf("%s: got %v, want ErrInvalidSplits", name, err)
			}
		}
	})

	t.Run("split drives category spend, exactly once", func(t *testing.T) {
		got, err := svc.ReplaceSplits(ctx, userID, tx.ID, []service.SplitInput{
			{CategoryID: &a, Amount: d("-60.05")},
			{CategoryID: &b, Amount: d("-40.05")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Splits) != 2 || !got.Amount.Equal(d("-100.10")) {
			t.Fatalf("splits=%d amount=%s", len(got.Splits), got.Amount)
		}
		from, to := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		spent, err := repository.NewBudgetRepository(g).SpendByCategoryInRange(ctx, userID, []int64{a, b}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if !spent[a].Equal(d("60.05")) || !spent[b].Equal(d("40.05")) {
			t.Fatalf("budget spend a=%s b=%s", spent[a], spent[b])
		}
		items, err := repository.NewDashboardRepository(g).SpendByCategory(ctx, userID, from, to)
		if err != nil {
			t.Fatal(err)
		}
		total := decimal.Zero
		for _, it := range items {
			total = total.Add(it.Amount)
		}
		if !total.Equal(d("100.10")) {
			t.Fatalf("category spend total %s, want 100.10 (no double count)", total)
		}
	})

	t.Run("amount locked while split", func(t *testing.T) {
		amt := d("-5")
		if _, err := svc.Update(ctx, userID, tx.ID, service.UpdateTransactionInput{Amount: &amt}); !errors.Is(err, service.ErrSplitAmountLocked) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("other user cannot split", func(t *testing.T) {
		other := seedTestUser(t, g)
		if _, err := svc.ReplaceSplits(ctx, other, tx.ID, nil); !errors.Is(err, service.ErrTransactionNotFound) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("empty clears and spend reverts to parent category", func(t *testing.T) {
		got, err := svc.ReplaceSplits(ctx, userID, tx.ID, []service.SplitInput{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Splits) != 0 {
			t.Fatalf("splits remain: %d", len(got.Splits))
		}
	})
}

// #445: Plaid revises the amount of a split transaction (pending $48 -> posted
// $50). The ledger follows the bank, the user's splits survive untouched, the
// row is flagged, and spend/budget totals fall back to the parent so nothing
// reports the stale allocation as fully allocated. Replays are idempotent and
// re-splitting (or the amount matching again) restores the allocation.
func TestTransactionService_SplitSurvivesAmountRevision(t *testing.T) {
	svc, userID, accountID, catA, g := newTxSvc(t)
	ctx := context.Background()
	catB := &model.Category{Name: "SplitRevB", Slug: "split-rev-b-" + time.Now().Format("150405.000000")}
	if err := g.Create(catB).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, catB.ID) })

	in := validTxInput(accountID)
	in.Amount = d("-48.00")
	in.TransactionDate = time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	tx, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, tx.ID) })
	ptxn := "ptxn-445-" + time.Now().Format("150405.000000")
	if err := g.Model(&model.Transaction{}).Where("id = ?", tx.ID).Update("plaid_transaction_id", ptxn).Error; err != nil {
		t.Fatal(err)
	}
	a, b := catA, catB.ID
	if _, err := svc.ReplaceSplits(ctx, userID, tx.ID, []service.SplitInput{
		{CategoryID: &a, Amount: d("-30.50")},
		{CategoryID: &b, Amount: d("-17.50")},
	}); err != nil {
		t.Fatal(err)
	}

	txRepo := repository.NewTransactionRepository(g)
	from, to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	spend := func() (sa, sb decimal.Decimal) {
		spent, err := repository.NewBudgetRepository(g).SpendByCategoryInRange(ctx, userID, []int64{a, b}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		return spent[a], spent[b]
	}
	revise := func(amount string) {
		cur, err := svc.Get(ctx, userID, tx.ID)
		if err != nil {
			t.Fatal(err)
		}
		cur.Amount = d(amount)
		if err := txRepo.UpdateByPlaidTransactionID(ctx, userID, ptxn, *cur); err != nil {
			t.Fatal(err)
		}
	}

	// Same amount replayed: nothing changes.
	revise("-48.00")
	got, _ := svc.Get(ctx, userID, tx.ID)
	if got.SplitsNeedAttention || len(got.Splits) != 2 {
		t.Fatalf("replay changed splits: attention=%v n=%d", got.SplitsNeedAttention, len(got.Splits))
	}
	if sa, sb := spend(); !sa.Equal(d("30.50")) || !sb.Equal(d("17.50")) {
		t.Fatalf("spend a=%s b=%s", sa, sb)
	}

	// Posted at a different amount, replayed twice.
	for i := 0; i < 2; i++ {
		revise("-50.00")
		got, err = svc.Get(ctx, userID, tx.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Amount.Equal(d("-50.00")) {
			t.Fatalf("ledger amount %s, want bank's -50.00", got.Amount)
		}
		if len(got.Splits) != 2 || !got.SplitsNeedAttention {
			t.Fatalf("want 2 preserved splits flagged, got n=%d attention=%v", len(got.Splits), got.SplitsNeedAttention)
		}
	}
	// Stale allocation is not reported: split categories get nothing, the
	// parent's full posted amount is counted exactly once.
	if sa, sb := spend(); !sa.IsZero() || !sb.IsZero() {
		t.Fatalf("stale split still driving spend a=%s b=%s", sa, sb)
	}
	items, err := repository.NewDashboardRepository(g).SpendByCategory(ctx, userID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	total := decimal.Zero
	for _, it := range items {
		total = total.Add(it.Amount)
	}
	if !total.Equal(d("50.00")) {
		t.Fatalf("category spend total %s, want 50.00", total)
	}

	// User re-splits to the new total: allocation resumes.
	if _, err := svc.ReplaceSplits(ctx, userID, tx.ID, []service.SplitInput{
		{CategoryID: &a, Amount: d("-32.50")},
		{CategoryID: &b, Amount: d("-17.50")},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Get(ctx, userID, tx.ID)
	if got.SplitsNeedAttention {
		t.Fatal("still flagged after re-split")
	}
	if sa, sb := spend(); !sa.Equal(d("32.50")) || !sb.Equal(d("17.50")) {
		t.Fatalf("after re-split a=%s b=%s", sa, sb)
	}
}
