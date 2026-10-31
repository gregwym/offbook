package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// TestAccountService_UpdateBalance_Backdated covers #669: a balance reported
// for a past day is compared with the ledger as of that day, keeps every
// later flow, and never moves a balance a later anchor already pinned.
func TestAccountService_UpdateBalance_Backdated(t *testing.T) {
	svc, userID, g := newAccountSvc(t)
	svc.WithValuation(valuation.NewService(
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewAssetRepository(g),
		repository.NewAccountRepository(g),
	))
	svc.WithBalanceObservationRepo(repository.NewBalanceObservationRepository(g))
	t.Cleanup(func() {
		g.Where("user_id = ?", userID).Delete(&model.AccountBalanceObservation{})
	})
	ctx := context.Background()
	d := decimal.RequireFromString
	txRepo := repository.NewTransactionRepository(g)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	d0 := today.AddDate(0, 0, -150)

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
	row := func(t *testing.T, a *model.Account, kind, amount string, on time.Time) {
		t.Helper()
		desc := kind
		if err := txRepo.Create(ctx, &model.Transaction{
			UserID: userID, AccountID: a.ID, AssetID: a.PrimaryQuoteAssetID, Kind: kind,
			Amount: d(amount), Description: &desc, TransactionDate: on, Source: "manual",
		}); err != nil {
			t.Fatalf("%s row: %v", kind, err)
		}
	}
	// The issue's ledger: opening 1,000 on d0, then −100 ten days later.
	seed := func(t *testing.T, i int) *model.Account {
		t.Helper()
		a := newAcct(t, i, "0")
		row(t, a, model.KindOpeningBalance, "1000", d0)
		row(t, a, model.KindFlow, "-100", d0.AddDate(0, 0, 10))
		return a
	}
	update := func(t *testing.T, a *model.Account, bal string, asOf *time.Time) *service.UpdateBalanceResult {
		t.Helper()
		res, err := svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d(bal), AsOf: asOf})
		if err != nil {
			t.Fatalf("update balance: %v", err)
		}
		return res
	}
	assertBalances := func(t *testing.T, a *model.Account, res *service.UpdateBalanceResult, at time.Time, historical, current string) {
		t.Helper()
		if !res.Account.Balance.Equal(d(current)) {
			t.Errorf("account balance = %s, want %s", res.Account.Balance, current)
		}
		hist, err := txRepo.FoldQuantityThrough(ctx, userID, a.ID, a.PrimaryQuoteAssetID, at)
		if err != nil {
			t.Fatal(err)
		}
		if !hist.Equal(d(historical)) {
			t.Errorf("balance as of %s = %s, want %s", at.Format("2006-01-02"), hist, historical)
		}
		fold, err := txRepo.FoldQuantity(ctx, userID, a.ID, a.PrimaryQuoteAssetID)
		if err != nil {
			t.Fatal(err)
		}
		var pos model.Position
		if err := g.Where("account_id = ? AND asset_id = ?", a.ID, a.PrimaryQuoteAssetID).First(&pos).Error; err != nil {
			t.Fatalf("position: %v", err)
		}
		if !fold.Equal(d(current)) || !pos.Quantity.Equal(fold) {
			t.Errorf("fold = %s, position = %s, want both %s", fold, pos.Quantity, current)
		}
	}
	reconciling := func(t *testing.T, a *model.Account) []model.Transaction {
		t.Helper()
		var out []model.Transaction
		if err := g.Where("account_id = ? AND kind = ?", a.ID, model.KindAdjustment).
			Order("transaction_date, id").Find(&out).Error; err != nil {
			t.Fatal(err)
		}
		return out
	}

	t.Run("correct past balance writes nothing and keeps later spending", func(t *testing.T) {
		a := seed(t, 21)
		for range 2 { // a repeated submission is still a no-op
			res := update(t, a, "1000", &d0)
			if res.Recorded != nil {
				t.Fatalf("recorded = %+v, want nothing", res.Recorded)
			}
			assertBalances(t, a, res, d0, "1000", "900")
		}
		var n int64
		g.Model(&model.AccountBalanceObservation{}).Where("account_id = ?", a.ID).Count(&n)
		if n != 2 {
			t.Errorf("observations = %d, want 2", n)
		}
	})

	t.Run("discrepant past balance adjusts that day and carries later flows", func(t *testing.T) {
		a := seed(t, 22)
		res := update(t, a, "1100", &d0)
		if res.Recorded == nil || res.Recorded.Kind != model.KindAdjustment || !res.Recorded.Amount.Equal(d("100")) {
			t.Fatalf("recorded = %+v, want adjustment 100", res.Recorded)
		}
		assertBalances(t, a, res, d0, "1100", "1000")
		adj := reconciling(t, a)
		if len(adj) != 1 || !adj[0].TransactionDate.Equal(d0) {
			t.Fatalf("adjustments = %+v, want one dated %s", adj, d0.Format("2006-01-02"))
		}
		again := update(t, a, "1100", &d0)
		if again.Recorded != nil || len(reconciling(t, a)) != 1 {
			t.Errorf("repeat wrote %+v, want nothing", again.Recorded)
		}
		assertBalances(t, a, again, d0, "1100", "1000")
	})

	t.Run("later observation keeps the balance it confirmed", func(t *testing.T) {
		a := seed(t, 23)
		if res := update(t, a, "900", nil); res.Recorded != nil {
			t.Fatalf("today's matching balance recorded %+v", res.Recorded)
		}
		res := update(t, a, "1100", &d0)
		if res.Recorded == nil || !res.Recorded.Amount.Equal(d("100")) {
			t.Fatalf("recorded = %+v, want adjustment 100", res.Recorded)
		}
		assertBalances(t, a, res, d0, "1100", "900")
		adj := reconciling(t, a)
		if len(adj) != 2 || !adj[1].Amount.Equal(d("-100")) || !adj[1].TransactionDate.Equal(today) {
			t.Fatalf("adjustments = %+v, want +100 on d0 and −100 today", adj)
		}
		if adj[1].CausedByObservationID == nil || *adj[1].CausedByObservationID != *adj[0].CausedByObservationID {
			t.Error("offset should link to the backdated observation that caused it")
		}
		again := update(t, a, "1100", &d0)
		if again.Recorded != nil || len(reconciling(t, a)) != 2 {
			t.Errorf("repeat wrote %+v, want nothing", again.Recorded)
		}

		// The reconciliation view attributes the backdated checkpoint to its
		// own +100 row, not to the offset.
		rep, err := service.NewReconciliationService(txRepo, repository.NewBalanceObservationRepository(g),
			repository.NewAccountRepository(g), 0.05).Report(ctx, userID, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, cp := range rep.Checkpoints {
			if cp.ObservationID == *adj[0].CausedByObservationID && (cp.TransactionID == nil || *cp.TransactionID != adj[0].ID) {
				t.Errorf("backdated checkpoint maps to %v, want row %d", cp.TransactionID, adj[0].ID)
			}
		}
	})

	t.Run("later opening anchor keeps the balance it set", func(t *testing.T) {
		a := newAcct(t, 24, "10") // opening balance anchored today
		past := today.AddDate(0, 0, -20)
		res := update(t, a, "12", &past)
		assertBalances(t, a, res, past, "12", "10")
	})

	// #673: the Accounts row says what was reported, so the response carries
	// the reported figure alongside today's derived balance.
	t.Run("response carries the reported balance, not today's", func(t *testing.T) {
		a := seed(t, 26)
		update(t, a, "1100", &d0)
		resp, err := svc.GetResponse(ctx, userID, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if resp.LastObservedAt == nil || !resp.LastObservedAt.Equal(d0) {
			t.Errorf("last_observed_at = %v, want %s", resp.LastObservedAt, d0)
		}
		if resp.LastObservedBalance == nil || !resp.LastObservedBalance.Equal(d("1100")) {
			t.Errorf("last_observed_balance = %v, want 1100", resp.LastObservedBalance)
		}
		if !resp.Balance.Equal(d("1000")) {
			t.Errorf("balance = %s, want 1000", resp.Balance)
		}
		// Another user never sees this account's observation.
		other := seedTestUser(t, g)
		list, _, err := svc.ListResponse(ctx, other, repository.AccountFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range list {
			if r.ID == a.ID {
				t.Errorf("other user listed account %d", a.ID)
			}
		}
	})

	t.Run("flows on the as_of day count toward it", func(t *testing.T) {
		a := newAcct(t, 25, "0")
		row(t, a, model.KindOpeningBalance, "1000", d0.AddDate(0, 0, -1))
		row(t, a, model.KindFlow, "-100", d0)
		row(t, a, model.KindFlow, "-50", d0.AddDate(0, 0, 1))
		res := update(t, a, "900", &d0)
		if res.Recorded != nil {
			t.Fatalf("recorded = %+v, want nothing", res.Recorded)
		}
		assertBalances(t, a, res, d0, "900", "850")
	})
}

// TestImportStatement_OlderStatementKeepsCurrentBalance covers the statement
// half of #669: importing an older statement after a newer one reconciles
// the older day without resetting the account to the older balance.
func TestImportStatement_OlderStatementKeepsCurrentBalance(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	svc = svc.WithStatementPositions(g, repository.NewAssetRepository(g))
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Position{}) })
	t.Cleanup(func() { g.Where("account_id = ?", accountID).Delete(&model.AccountBalanceObservation{}) })
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	cash := usdID(t, g, accountID)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	older, newer := today.AddDate(0, 0, -60), today.AddDate(0, 0, -30)

	statement := func(closing string, at time.Time) *ingestion.Extraction {
		bal, when, ok := ingestion.ParseClosingBalance(closing, at.Format("2006-01-02"))
		if !ok {
			t.Fatalf("parse closing balance %s", closing)
		}
		return &ingestion.Extraction{Source: "pdf", ClosingBalance: &bal, ClosingBalanceAt: &when}
	}
	position := func(t *testing.T, g *gorm.DB) decimal.Decimal {
		t.Helper()
		var pos model.Position
		if err := g.Where("account_id = ? AND asset_id = ?", accountID, cash).First(&pos).Error; err != nil {
			t.Fatalf("position: %v", err)
		}
		return pos.Quantity
	}

	if _, err := svc.ImportStatement(ctx, userID, accountID, statement("500", newer), true); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.ImportStatement(ctx, userID, accountID, statement("300", older), false)
	if err != nil {
		t.Fatal(err)
	}
	// The newer statement's opening anchor is the balance before the first
	// known row, so it already counts on the older day (#680): 300 − 500.
	if preview.ClosingBalance == nil || !preview.ClosingBalance.Effect.Adjustment.Equal(decimal.NewFromInt(-200)) {
		t.Fatalf("preview = %+v, want adjustment −200 against the older day's ledger", preview.ClosingBalance)
	}
	for range 2 { // re-importing the older statement has no further effect
		if _, err := svc.ImportStatement(ctx, userID, accountID, statement("300", older), true); err != nil {
			t.Fatal(err)
		}
		assertFold(t, txRepo, userID, accountID, cash, "500")
		if q := position(t, g); !q.Equal(decimal.NewFromInt(500)) {
			t.Errorf("position = %s, want 500 (the newer statement's balance)", q)
		}
		at, err := txRepo.FoldQuantityThrough(ctx, userID, accountID, cash, older)
		if err != nil {
			t.Fatal(err)
		}
		if !at.Equal(decimal.NewFromInt(300)) {
			t.Errorf("balance as of older statement = %s, want 300", at)
		}
	}
}

// historyAt is the balance the net-worth history reader (FoldByUserAsOf, the
// dashboard's) shows for the user's assetID on day. Callers give each case
// its own user, so the user-wide fold is this one account's.
func historyAt(t *testing.T, txRepo repository.TransactionRepository, userID, assetID int64, day time.Time) decimal.Decimal {
	t.Helper()
	positions, err := txRepo.FoldByUserAsOf(context.Background(), userID, day)
	if err != nil {
		t.Fatalf("fold by user as of %s: %v", day.Format("2006-01-02"), err)
	}
	sum := decimal.Zero
	for _, p := range positions {
		if p.AssetID == assetID {
			sum = sum.Add(p.Quantity)
		}
	}
	return sum
}

// TestUpdateBalance_BackdatedMatchesNetWorthHistory covers #680 case 1: a
// backdated balance must read back as reported in net-worth history, not
// only through the reconciliation helper. The opening anchor counts as the
// balance before the first known row in both.
func TestUpdateBalance_BackdatedMatchesNetWorthHistory(t *testing.T) {
	svc, _, g := newAccountSvc(t)
	svc.WithValuation(valuation.NewService(
		repository.NewPositionRepository(g),
		repository.NewPriceRepository(g),
		repository.NewAssetRepository(g),
		repository.NewAccountRepository(g),
	))
	svc.WithBalanceObservationRepo(repository.NewBalanceObservationRepository(g))
	ctx := context.Background()
	d := decimal.RequireFromString
	txRepo := repository.NewTransactionRepository(g)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	d0 := today.AddDate(0, 0, -150)

	type step struct {
		balance string
		asOf    time.Time
	}
	cases := []struct {
		name    string
		opening string // via account creation, dated today
		rows    []model.Transaction
		steps   []step
		want    map[time.Time]string // history reader by day
		current string
	}{
		{
			name:    "opening today, higher balance twenty days ago",
			opening: "10",
			steps:   []step{{"12", today.AddDate(0, 0, -20)}},
			want:    map[time.Time]string{today.AddDate(0, 0, -20): "12", today.AddDate(0, 0, -1): "12", today: "10"},
			current: "10",
		},
		{
			name: "earlier report before a dated opening anchor",
			rows: []model.Transaction{
				{Kind: model.KindOpeningBalance, Amount: d("1000"), TransactionDate: d0},
				{Kind: model.KindFlow, Amount: d("-100"), TransactionDate: d0.AddDate(0, 0, 10)},
			},
			steps:   []step{{"800", d0.AddDate(0, 0, -10)}},
			want:    map[time.Time]string{d0.AddDate(0, 0, -10): "800", d0: "1000", d0.AddDate(0, 0, 10): "900"},
			current: "900",
		},
		{
			name: "issue #669 ledger keeps its history",
			rows: []model.Transaction{
				{Kind: model.KindOpeningBalance, Amount: d("1000"), TransactionDate: d0},
				{Kind: model.KindFlow, Amount: d("-100"), TransactionDate: d0.AddDate(0, 0, 10)},
			},
			steps:   []step{{"1100", d0}, {"1100", d0}},
			want:    map[time.Time]string{d0: "1100", d0.AddDate(0, 0, 10): "1000"},
			current: "1000",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := seedTestUser(t, g)
			t.Cleanup(func() {
				g.Where("user_id = ?", userID).Delete(&model.AccountBalanceObservation{})
			})
			in := validInput(tcSuffix(i))
			in.OpeningBalance = d("0")
			if tc.opening != "" {
				in.OpeningBalance = d(tc.opening)
			}
			a, err := svc.Create(ctx, userID, in)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			for _, r := range tc.rows {
				desc := r.Kind
				r.UserID, r.AccountID, r.AssetID, r.Description, r.Source = userID, a.ID, a.PrimaryQuoteAssetID, &desc, "manual"
				if err := txRepo.Create(ctx, &r); err != nil {
					t.Fatalf("seed row: %v", err)
				}
			}
			var res *service.UpdateBalanceResult
			for _, st := range tc.steps {
				asOf := st.asOf
				if res, err = svc.UpdateBalance(ctx, userID, a.ID, service.UpdateBalanceInput{Balance: d(st.balance), AsOf: &asOf}); err != nil {
					t.Fatalf("update balance: %v", err)
				}
				// Whatever reader is asked, the reported day reads as reported.
				if got := historyAt(t, txRepo, userID, a.PrimaryQuoteAssetID, asOf); !got.Equal(d(st.balance)) {
					t.Errorf("history on reported day %s = %s, want %s", asOf.Format("2006-01-02"), got, st.balance)
				}
				if got, err := txRepo.FoldQuantityThrough(ctx, userID, a.ID, a.PrimaryQuoteAssetID, asOf); err != nil || !got.Equal(d(st.balance)) {
					t.Errorf("reconciliation fold on %s = %s (%v), want %s", asOf.Format("2006-01-02"), got, err, st.balance)
				}
			}
			for day, want := range tc.want {
				if got := historyAt(t, txRepo, userID, a.PrimaryQuoteAssetID, day); !got.Equal(d(want)) {
					t.Errorf("history on %s = %s, want %s", day.Format("2006-01-02"), got, want)
				}
			}
			if !res.Account.Balance.Equal(d(tc.current)) {
				t.Errorf("current balance = %s, want %s", res.Account.Balance, tc.current)
			}
			assertFold(t, txRepo, userID, a.ID, a.PrimaryQuoteAssetID, tc.current)
		})
	}
}

// TestImportStatement_OlderStatementRowsKeepLaterBalance covers #680 case 2:
// an older statement's new flows must not move a balance a newer statement
// already confirmed, whether or not its own closing balance needed a
// correction. Re-importing it changes nothing.
func TestImportStatement_OlderStatementRowsKeepLaterBalance(t *testing.T) {
	for _, tc := range []struct {
		name, olderClosing string
	}{
		{"older balance needs a correction", "300"},
		{"older balance already matches", "470"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, userID, accountID, g := newImportSvc(t)
			svc = svc.WithStatementPositions(g, repository.NewAssetRepository(g))
			t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Position{}) })
			t.Cleanup(func() { g.Where("account_id = ?", accountID).Delete(&model.AccountBalanceObservation{}) })
			ctx := context.Background()
			txRepo := repository.NewTransactionRepository(g)
			cash := usdID(t, g, accountID)
			today := time.Now().UTC().Truncate(24 * time.Hour)
			older, newer := today.AddDate(0, 0, -60), today.AddDate(0, 0, -30)
			d := decimal.RequireFromString

			statement := func(closing string, at time.Time, rows ...ingestion.ParsedRow) *ingestion.Extraction {
				bal, when, ok := ingestion.ParseClosingBalance(closing, at.Format("2006-01-02"))
				if !ok {
					t.Fatalf("parse closing balance %s", closing)
				}
				return &ingestion.Extraction{Source: "pdf", Rows: rows, ClosingBalance: &bal, ClosingBalanceAt: &when}
			}
			spend := ingestion.ParsedRow{Line: 2, Date: older.AddDate(0, 0, -10), Amount: d("-30"), Description: "Hardware store", Confidence: 1}

			if _, err := svc.ImportStatement(ctx, userID, accountID, statement("500", newer), true); err != nil {
				t.Fatal(err)
			}
			for i := range 2 { // the second import is a re-import: every row is a duplicate
				res, err := svc.ImportStatement(ctx, userID, accountID, statement(tc.olderClosing, older, spend), true)
				if err != nil {
					t.Fatal(err)
				}
				if wantNew := 1 - i; res.InsertedCount != wantNew {
					t.Errorf("import %d inserted %d rows, want %d", i+1, res.InsertedCount, wantNew)
				}
				assertFold(t, txRepo, userID, accountID, cash, "500")
				var pos model.Position
				if err := g.Where("account_id = ? AND asset_id = ?", accountID, cash).First(&pos).Error; err != nil {
					t.Fatalf("position: %v", err)
				}
				if !pos.Quantity.Equal(d("500")) {
					t.Errorf("position = %s, want 500 (the newer statement's balance)", pos.Quantity)
				}
				for day, want := range map[time.Time]string{older: tc.olderClosing, newer: "500"} {
					if got := historyAt(t, txRepo, userID, cash, day); !got.Equal(d(want)) {
						t.Errorf("history on %s = %s, want %s", day.Format("2006-01-02"), got, want)
					}
				}
			}
		})
	}
}
