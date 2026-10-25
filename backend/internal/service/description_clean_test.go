package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// TestDescriptionClean_TopMerchantsGroupsStoreNumbers (#588): two statement
// rows from one café that differ only by store number are one merchant,
// summed exactly, under the cleaned name. A row with a merchant name keeps
// it, and the raw description is never rewritten.
func TestDescriptionClean_TopMerchantsGroupsStoreNumbers(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()
	d := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	seedMerchantTxn(t, g, userID, accountID, "", nil, nil, new("SQ *BLUE BTL 0412"), nil, d, decimal.RequireFromString("-6.50"), false)
	seedMerchantTxn(t, g, userID, accountID, "", nil, nil, new("SQ *BLUE BTL 0415"), nil, d, decimal.RequireFromString("-7.25"), false)
	seedMerchantTxn(t, g, userID, accountID, "", new("Corner Grocer"), nil, new("CORNER GROCER 0099"), nil, d, decimal.RequireFromString("-3.10"), false)

	items, err := svc.TopMerchants(ctx, userID, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopMerchants: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d merchants, want 2; items=%+v", len(items), items)
	}
	if items[0].Merchant != "Blue Btl" || items[0].Amount != "13.75" || items[0].Count != 2 {
		t.Errorf("first merchant = %+v, want {Blue Btl 13.75 2}", items[0])
	}
	if items[1].Merchant != "Corner Grocer" || items[1].Count != 1 {
		t.Errorf("second merchant = %+v, want Corner Grocer ×1", items[1])
	}

	var rows []model.Transaction
	if err := g.Where("account_id = ?", accountID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	if *rows[0].Description != "SQ *BLUE BTL 0412" || *rows[1].Description != "SQ *BLUE BTL 0415" {
		t.Errorf("raw descriptions rewritten: %q, %q", *rows[0].Description, *rows[1].Description)
	}
	if rows[2].DescriptionClean != nil {
		t.Errorf("row with a merchant name got description_clean %q, want nil", *rows[2].DescriptionClean)
	}
}

// TestDescriptionClean_RecurringClustersDatedDescriptors (#588): a
// subscription whose bank string carries the billing date each month is
// still detected as one monthly recurring charge.
func TestDescriptionClean_RecurringClustersDatedDescriptors(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	for i, desc := range []string{"NETFLIX.COM 0215", "NETFLIX.COM 0315", "NETFLIX.COM 0415"} {
		d := time.Date(2026, time.Month(2+i), 15, 0, 0, 0, 0, time.UTC)
		seedMerchantTxn(t, g, userID, accountID, "", nil, nil, new(desc), nil, d, decimal.RequireFromString("-15.49"), false)
	}

	items, err := svc.Recurring(ctx, userID)
	if err != nil {
		t.Fatalf("Recurring: %v", err)
	}
	item, ok := findRecurring(items, "Netflix.com")
	if !ok {
		t.Fatalf("Netflix.com not detected as recurring; items=%+v", items)
	}
	if item.Cadence != "monthly" || item.Occurrences != 3 || item.LastAmount != "15.49" {
		t.Errorf("recurring = %+v, want monthly ×3 at 15.49", item)
	}
}

// TestDescriptionClean_SkipsNonFlowAndExplicitValues: the insert hook only
// fills flow rows, and never overrides a description_clean the caller set.
func TestDescriptionClean_SkipsNonFlowAndExplicitValues(t *testing.T) {
	_, userID, accountID, g := newDashboardSvc(t)
	usdID := testutil.LookupUSDAssetID(t, g)
	d := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	leg := &model.Transaction{
		UserID: userID, AccountID: accountID, AssetID: usdID, Kind: model.KindTradeLeg,
		Description: new("BUY VTI 0412"), Amount: decimal.NewFromInt(-100), TransactionDate: d, Source: "manual",
	}
	explicit := &model.Transaction{
		UserID: userID, AccountID: accountID, Description: new("SQ *BLUE BTL 0412"), DescriptionClean: new("Blue Bottle"),
		Amount: decimal.NewFromInt(-5), TransactionDate: d, Source: "csv",
	}
	for _, tx := range []*model.Transaction{leg, explicit} {
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if leg.DescriptionClean != nil {
		t.Errorf("trade leg got description_clean %q, want nil", *leg.DescriptionClean)
	}
	if *explicit.DescriptionClean != "Blue Bottle" {
		t.Errorf("explicit description_clean overwritten: %q", *explicit.DescriptionClean)
	}
}

// TestBackfillDescriptionClean_FillsLegacyRowsOnce: rows written before the
// hook get a cleaned label; amounts, raw description and merchant-named rows
// stay untouched, and a second run changes nothing.
func TestBackfillDescriptionClean_FillsLegacyRowsOnce(t *testing.T) {
	_, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()
	d := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	seedMerchantTxn(t, g, userID, accountID, "", nil, nil, new("TST* JOE'S PIZZA #22"), nil, d, decimal.RequireFromString("-18.40"), false)
	seedMerchantTxn(t, g, userID, accountID, "", new("Joe's"), nil, new("TST* JOE'S PIZZA #23"), nil, d, decimal.RequireFromString("-9.00"), false)
	// Simulate pre-#588 rows: no cleaned label.
	if err := g.Exec(`UPDATE transactions SET description_clean = NULL WHERE account_id = ?`, accountID).Error; err != nil {
		t.Fatalf("reset: %v", err)
	}

	if _, err := repository.BackfillDescriptionClean(ctx, g); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	var rows []model.Transaction
	if err := g.Where("account_id = ?", accountID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	if rows[0].DescriptionClean == nil || *rows[0].DescriptionClean != "Joe's Pizza" {
		t.Errorf("legacy row description_clean = %v, want Joe's Pizza", rows[0].DescriptionClean)
	}
	if *rows[0].Description != "TST* JOE'S PIZZA #22" || !rows[0].Amount.Equal(decimal.RequireFromString("-18.40")) {
		t.Errorf("legacy row changed: %q %s", *rows[0].Description, rows[0].Amount)
	}
	if rows[1].DescriptionClean != nil {
		t.Errorf("merchant-named row got description_clean %q, want nil", *rows[1].DescriptionClean)
	}

	n, err := repository.BackfillDescriptionClean(ctx, g)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if n != 0 {
		t.Errorf("second backfill updated %d rows, want 0", n)
	}
}
