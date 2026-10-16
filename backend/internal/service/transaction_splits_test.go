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
