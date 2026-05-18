package plaid_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
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
		Amount:             decimal.NewFromFloat(-50.00), // initial pending amount
		Currency:           "USD",
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

	merged, err := plaidsvc.MergePlaidUpdate(existing, incoming, 11, nil, nil)
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
		Amount:             decimal.NewFromFloat(-1),
		Currency:           "USD",
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
	merged, err := plaidsvc.MergePlaidUpdate(existing, incoming, 1, nil, nil)
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
