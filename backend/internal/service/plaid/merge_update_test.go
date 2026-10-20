package plaid_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

func TestMergePlaidUpdate_PendingToPosted(t *testing.T) {
	// Existing pending row: user has already categorized it and added a note.
	categoryID := int64(42)
	notes := "Date night with Sam"
	existing := model.Transaction{
		ID:                 999,
		UserID:             7,
		AccountID:          11,
		AssetID:            1,
		Amount:             decimal.NewFromFloat(-50.00), // initial pending amount
		Description:        ptr("Restaurant pending"),
		TransactionDate:    mustDate("2026-05-10"),
		Source:             "plaid",
		PlaidTransactionID: ptr("ptx-1"),
		CategoryID:         &categoryID,
		Notes:              &notes,
		IsTransfer:         false,
	}

	// Plaid now reports the cleared amount and a different merchant name.
	auth := "2026-05-10"
	merchant := "Tartine SF"
	incoming := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-1",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromFloat(52.13), // posted (Plaid +ve = outflow)
		Currency:           "USD",
		Name:               "TARTINE BAKERY",
		MerchantName:       &merchant,
		Date:               "2026-05-11", // settled date
		AuthorizedDate:     &auth,
	}

	merged, err := plaidsvc.MergePlaidUpdate(existing, incoming, 11, 1, nil, nil)
	if err != nil {
		t.Fatalf("MergePlaidUpdate: %v", err)
	}

	// Plaid fields overwritten.
	if !merged.Amount.Equal(decimal.NewFromFloat(-52.13)) {
		t.Errorf("amount = %s, want -52.13 (sign-flipped from 52.13)", merged.Amount)
	}
	if merged.Description == nil || *merged.Description != "TARTINE BAKERY" {
		t.Errorf("description = %v, want TARTINE BAKERY", merged.Description)
	}
	if merged.MerchantName == nil || *merged.MerchantName != "Tartine SF" {
		t.Errorf("merchant = %v", merged.MerchantName)
	}
	if merged.TransactionDate.Format("2006-01-02") != "2026-05-11" {
		t.Errorf("transaction_date = %s", merged.TransactionDate)
	}
	if merged.PostedDate == nil || merged.PostedDate.Format("2006-01-02") != "2026-05-10" {
		t.Errorf("posted_date = %v", merged.PostedDate)
	}

	// User-edited fields preserved.
	if merged.CategoryID == nil || *merged.CategoryID != 42 {
		t.Errorf("category_id = %v, want 42 (preserved)", merged.CategoryID)
	}
	if merged.Notes == nil || *merged.Notes != "Date night with Sam" {
		t.Errorf("notes = %v, want preserved", merged.Notes)
	}
	// Identity fields preserved.
	if merged.ID != 999 || merged.UserID != 7 {
		t.Errorf("identity fields rewritten: id=%d user=%d", merged.ID, merged.UserID)
	}
}

func TestMergePlaidUpdate_NilFieldsClearable(t *testing.T) {
	// Edge case: existing row has a merchant_name, Plaid removes it
	// (their data refresh dropped it). We want the merged row to also
	// drop the merchant_name — Plaid is authoritative for that field.
	prior := "Old Merchant"
	existing := model.Transaction{
		ID:                 1,
		UserID:             1,
		AccountID:          1,
		AssetID:            1,
		Amount:             decimal.NewFromFloat(-1),
		MerchantName:       &prior,
		TransactionDate:    mustDate("2026-05-10"),
		Source:             "plaid",
		PlaidTransactionID: ptr("ptx-x"),
	}
	incoming := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-x",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromFloat(1),
		Currency:           "USD",
		Name:               "X",
		Date:               "2026-05-10",
		// MerchantName intentionally nil
	}
	merged, err := plaidsvc.MergePlaidUpdate(existing, incoming, 1, 1, nil, nil)
	if err != nil {
		t.Fatalf("MergePlaidUpdate: %v", err)
	}
	if merged.MerchantName != nil {
		t.Errorf("merchant_name = %v, want nil (Plaid is authoritative)", merged.MerchantName)
	}
}

func ptr[T any](v T) *T { return &v }

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// #424: a pending→posted amount change (or any resync) must never reset the
// user's review state — reviewed rows stay reviewed, unreviewed stay NULL.
func TestMergePlaidUpdate_PreservesReviewedAt(t *testing.T) {
	reviewed := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	incoming := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-9",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromFloat(10.00),
		Currency:           "USD",
		Name:               "COFFEE",
		Date:               "2026-05-11",
	}
	for _, tc := range []struct {
		name string
		in   *time.Time
	}{{"reviewed", &reviewed}, {"unreviewed", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			existing := model.Transaction{ID: 1, UserID: 7, AccountID: 11, AssetID: 1, Source: "plaid",
				PlaidTransactionID: ptr("ptx-9"), TransactionDate: mustDate("2026-05-10"), ReviewedAt: tc.in}
			merged, err := plaidsvc.MergePlaidUpdate(existing, incoming, 11, 1, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if (merged.ReviewedAt == nil) != (tc.in == nil) || (tc.in != nil && !merged.ReviewedAt.Equal(*tc.in)) {
				t.Errorf("reviewed_at = %v, want %v", merged.ReviewedAt, tc.in)
			}
		})
	}
}

// #465: a Plaid resync (e.g. pending→posted amount change) must never unhide a
// transaction the user excluded from spending, and must not hide a visible one.
func TestMergePlaidUpdate_PreservesHiddenAt(t *testing.T) {
	hidden := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	incoming := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-10",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromFloat(12.34),
		Currency:           "USD",
		Name:               "COFFEE",
		Date:               "2026-05-11",
	}
	for _, tc := range []struct {
		name string
		in   *time.Time
	}{{"hidden", &hidden}, {"visible", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			existing := model.Transaction{ID: 1, UserID: 7, AccountID: 11, AssetID: 1, Source: "plaid",
				PlaidTransactionID: ptr("ptx-10"), TransactionDate: mustDate("2026-05-10"), HiddenAt: tc.in}
			merged, err := plaidsvc.MergePlaidUpdate(existing, incoming, 11, 1, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if (merged.HiddenAt == nil) != (tc.in == nil) || (tc.in != nil && !merged.HiddenAt.Equal(*tc.in)) {
				t.Errorf("hidden_at = %v, want %v", merged.HiddenAt, tc.in)
			}
		})
	}
}

// TestMergePlaidUpdate_RuleRenameSurvivesModify (#534): a modified payload
// carries Plaid's raw merchant again, but the user's rename rule re-runs in
// the mapping, so the clean merchant survives resync and pending→posted.
func TestMergePlaidUpdate_RuleRenameSurvivesModify(t *testing.T) {
	raw := "AMZN Mktp US"
	clean := "Amazon"
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 5, CategoryID: 9, MatchType: "contains", Pattern: "AMZN", IsActive: true, RenameMerchant: &clean},
	})
	in := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-r", PlaidAccountID: "pa", Amount: decimal.RequireFromString("12.34"),
		Name: "AMZN MKTP US*2K4", MerchantName: &raw, Date: "2026-05-16",
	}
	added, err := plaidsvc.MapPlaidTransaction(in, 1, 11, 1, nil, rules)
	if err != nil {
		t.Fatalf("MapPlaidTransaction: %v", err)
	}
	if added.MerchantName == nil || *added.MerchantName != clean {
		t.Fatalf("added merchant = %v, want Amazon", added.MerchantName)
	}
	in.Amount = decimal.RequireFromString("12.99")
	merged, err := plaidsvc.MergePlaidUpdate(added, in, 11, 1, nil, rules)
	if err != nil {
		t.Fatalf("MergePlaidUpdate: %v", err)
	}
	if merged.MerchantName == nil || *merged.MerchantName != clean {
		t.Errorf("merged merchant = %v, want Amazon", merged.MerchantName)
	}
	if !merged.Amount.Equal(decimal.RequireFromString("-12.99")) {
		t.Errorf("merged amount = %s, want -12.99", merged.Amount)
	}
}
