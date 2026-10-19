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
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// TestAccountService_UpdateBalance covers the manual "update balance" path
// (#504): a reported balance becomes an observation plus the reconciling
// ledger row, the account balance lands exactly on it, and flow analytics are
// untouched.
func TestAccountService_UpdateBalance(t *testing.T) {
	svc, userID, g := newAccountSvc(t)
	svc.WithValuation(valuation.NewService(
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewAssetRepository(g),
		repository.NewAccountRepository(g),
	))
	t.Cleanup(func() {
		g.Where("user_id = ?", userID).Delete(&model.AccountBalanceObservation{})
	})
	ctx := context.Background()
	d := decimal.RequireFromString

	newAcct := func(t *testing.T, i int, opening string) *model.Account {
		t.Helper()
		in := validInput(tcSuffix(i))
		in.OpeningBalance = d(opening)
		a, err := svc.Create(ctx, userID, in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return a
	}
	flow := func(t *testing.T, a *model.Account, amount string) {
		t.Helper()
		desc := "flow"
		if err := repository.NewTransactionRepository(g).Create(ctx, &model.Transaction{
			UserID: userID, AccountID: a.ID, AssetID: a.PrimaryQuoteAssetID, Kind: model.KindFlow,
			Amount: d(amount), Description: &desc, TransactionDate: time.Now().UTC(), Source: "manual",
		}); err != nil {
			t.Fatalf("flow: %v", err)
		}
	}
	observations := func(t *testing.T, a *model.Account) []model.AccountBalanceObservation {
		t.Helper()
		var obs []model.AccountBalanceObservation
		if err := g.Where("account_id = ?", a.ID).Order("id").Find(&obs).Error; err != nil {
			t.Fatalf("observations: %v", err)
		}
		return obs
	}

	t.Run("first update without anchor writes opening balance", func(t *testing.T) {
		a := newAcct(t, 11, "0")
		flow(t, a, "-40.25")
		res, err := svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d("959.75")})
		if err != nil {
			t.Fatalf("update balance: %v", err)
		}
		if res.Recorded == nil || res.Recorded.Kind != model.KindOpeningBalance || !res.Recorded.Amount.Equal(d("1000")) {
			t.Fatalf("recorded = %+v, want opening_balance 1000", res.Recorded)
		}
		if !res.Account.Balance.Equal(d("959.75")) {
			t.Errorf("balance = %s, want 959.75", res.Account.Balance)
		}
		obs := observations(t, a)
		if len(obs) != 1 || obs[0].Source != "manual" || !obs[0].ObservedQuantity.Equal(d("959.75")) {
			t.Fatalf("observations = %+v, want one manual 959.75", obs)
		}
	})

	t.Run("later update writes adjustment linked to its observation", func(t *testing.T) {
		a := newAcct(t, 12, "500.10")
		flow(t, a, "-0.10")
		res, err := svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d("487.333333333333333333")})
		if err != nil {
			t.Fatalf("update balance: %v", err)
		}
		want := d("-12.666666666666666667")
		if res.Recorded == nil || res.Recorded.Kind != model.KindAdjustment || !res.Recorded.Amount.Equal(want) {
			t.Fatalf("recorded = %+v, want adjustment %s", res.Recorded, want)
		}
		if !res.Account.Balance.Equal(d("487.333333333333333333")) {
			t.Errorf("balance = %s, want exact 487.333333333333333333", res.Account.Balance)
		}
		var adj model.Transaction
		if err := g.First(&adj, res.Recorded.ID).Error; err != nil {
			t.Fatalf("load adjustment: %v", err)
		}
		obs := observations(t, a)
		if adj.CausedByObservationID == nil || len(obs) != 1 || *adj.CausedByObservationID != obs[0].ID {
			t.Errorf("adjustment caused_by = %v, want observation %v", adj.CausedByObservationID, obs)
		}
		var pos model.Position
		if err := g.Where("account_id = ? AND asset_id = ?", a.ID, a.PrimaryQuoteAssetID).First(&pos).Error; err != nil {
			t.Fatalf("position: %v", err)
		}
		if !pos.Quantity.Equal(d("487.333333333333333333")) {
			t.Errorf("position = %s, want 487.333333333333333333", pos.Quantity)
		}
	})

	t.Run("matching balance records observation and no ledger row", func(t *testing.T) {
		a := newAcct(t, 13, "250")
		res, err := svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d("250.00")})
		if err != nil {
			t.Fatalf("update balance: %v", err)
		}
		if res.Recorded != nil {
			t.Errorf("recorded = %+v, want nil", res.Recorded)
		}
		if len(observations(t, a)) != 1 {
			t.Error("want the observation recorded even when the ledger already matches")
		}
	})

	t.Run("past as_of is kept; future is rejected", func(t *testing.T) {
		a := newAcct(t, 14, "10")
		past := time.Now().UTC().AddDate(0, 0, -20)
		if _, err := svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d("12"), AsOf: &past}); err != nil {
			t.Fatalf("update balance: %v", err)
		}
		obs := observations(t, a)
		if len(obs) != 1 || obs[0].AsOf.UTC().Format("2006-01-02") != past.Format("2006-01-02") {
			t.Errorf("as_of = %v, want %s", obs, past.Format("2006-01-02"))
		}
		future := time.Now().UTC().AddDate(0, 0, 3)
		_, err := svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d("1"), AsOf: &future})
		if !errors.Is(err, service.ErrBalanceAsOfInFuture) {
			t.Errorf("err = %v, want ErrBalanceAsOfInFuture", err)
		}
	})

	t.Run("plaid-linked account is refused", func(t *testing.T) {
		a := newAcct(t, 15, "0")
		pid := "plaid-acct-" + tcSuffix(15)
		if err := g.Model(&model.Account{}).Where("id = ?", a.ID).Update("plaid_account_id", pid).Error; err != nil {
			t.Fatalf("mark plaid: %v", err)
		}
		_, err := svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d("1")})
		if !errors.Is(err, service.ErrAccountPlaidManaged) {
			t.Errorf("err = %v, want ErrAccountPlaidManaged", err)
		}
		if len(observations(t, a)) != 0 {
			t.Error("plaid account must not get a manual observation")
		}
	})

	t.Run("another user's account is not found", func(t *testing.T) {
		a := newAcct(t, 16, "5")
		other := seedTestUser(t, g)
		_, err := svc.UpdateBalance(ctx, other, a.ID, service.UpdateBalanceInput{Balance: d("1")})
		if !errors.Is(err, service.ErrAccountNotFound) {
			t.Errorf("err = %v, want ErrAccountNotFound", err)
		}
		if len(observations(t, a)) != 0 {
			t.Error("cross-user update must not write")
		}
	})
}
