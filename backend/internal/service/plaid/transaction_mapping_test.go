package plaid_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

func TestMapPlaidTransaction_SignFlipAndDates(t *testing.T) {
	authDate := "2026-05-15"
	merchant := "Blue Bottle Coffee"
	in := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-1",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromFloat(5.43), // Plaid: positive = outflow
		Currency:           "USD",
		Name:               "Blue Bottle SF",
		MerchantName:       &merchant,
		Date:               "2026-05-16",
		AuthorizedDate:     &authDate,
	}
	got, err := plaidsvc.MapPlaidTransaction(in, 7, 42)
	if err != nil {
		t.Fatalf("MapPlaidTransaction: %v", err)
	}

	if got.UserID != 7 || got.AccountID != 42 {
		t.Errorf("user/account = %d/%d, want 7/42", got.UserID, got.AccountID)
	}
	// Sign flip: 5.43 → -5.43
	if !got.Amount.Equal(decimal.NewFromFloat(-5.43)) {
		t.Errorf("amount = %s, want -5.43 (sign flipped)", got.Amount.String())
	}
	if got.Source != "plaid" {
		t.Errorf("source = %q, want plaid", got.Source)
	}
	if got.PlaidTransactionID == nil || *got.PlaidTransactionID != "ptx-1" {
		t.Errorf("plaid_transaction_id = %v", got.PlaidTransactionID)
	}
	if got.ExternalID == nil || *got.ExternalID != "ptx-1" {
		t.Errorf("external_id = %v (should mirror plaid_transaction_id)", got.ExternalID)
	}
	if got.Description == nil || *got.Description != "Blue Bottle SF" {
		t.Errorf("description = %v", got.Description)
	}
	if got.MerchantName == nil || *got.MerchantName != "Blue Bottle Coffee" {
		t.Errorf("merchant = %v", got.MerchantName)
	}
	if got.TransactionDate.Format("2006-01-02") != "2026-05-16" {
		t.Errorf("transaction_date = %s", got.TransactionDate)
	}
	if got.PostedDate == nil || got.PostedDate.Format("2006-01-02") != "2026-05-15" {
		t.Errorf("posted_date = %v", got.PostedDate)
	}
}

func TestMapPlaidTransaction_NilAuthorizedDate(t *testing.T) {
	in := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-2",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromFloat(-100), // refund: Plaid negative = inflow
		Currency:           "USD",
		Name:               "Payroll deposit",
		Date:               "2026-05-01",
	}
	got, err := plaidsvc.MapPlaidTransaction(in, 1, 2)
	if err != nil {
		t.Fatalf("MapPlaidTransaction: %v", err)
	}
	if !got.Amount.Equal(decimal.NewFromInt(100)) {
		t.Errorf("amount = %s, want 100 (refund: sign flipped to positive)", got.Amount.String())
	}
	if got.PostedDate != nil {
		t.Errorf("posted_date = %v, want nil when authorized_date absent", got.PostedDate)
	}
}

func TestMapPlaidTransaction_PrecisionPreserved(t *testing.T) {
	in := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-3",
		PlaidAccountID:     "pacct-1",
		// Choose a value that hurts when round-tripped through float.
		Amount:   decimal.RequireFromString("0.123456789012345"),
		Currency: "USD",
		Name:     "Micro txn",
		Date:     "2026-05-01",
	}
	got, err := plaidsvc.MapPlaidTransaction(in, 1, 2)
	if err != nil {
		t.Fatalf("MapPlaidTransaction: %v", err)
	}
	want := decimal.RequireFromString("-0.123456789012345")
	if !got.Amount.Equal(want) {
		t.Errorf("amount precision lost: got %s, want %s", got.Amount, want)
	}
}

func TestMapPlaidTransaction_RejectsBadDate(t *testing.T) {
	in := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-bad",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromInt(1),
		Date:               "not-a-date",
	}
	if _, err := plaidsvc.MapPlaidTransaction(in, 1, 2); err == nil {
		t.Fatal("expected parse error for malformed date")
	}
}

// Sanity check the year field of an existing time so the date column
// won't be silently typed as something weird.
func TestMapPlaidTransaction_DateIsUTC(t *testing.T) {
	in := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-tz",
		PlaidAccountID:     "pacct-1",
		Amount:             decimal.NewFromInt(1),
		Date:               "2026-01-31",
	}
	got, err := plaidsvc.MapPlaidTransaction(in, 1, 2)
	if err != nil {
		t.Fatalf("MapPlaidTransaction: %v", err)
	}
	if got.TransactionDate.Location() != time.UTC {
		t.Errorf("transaction_date location = %v, want UTC", got.TransactionDate.Location())
	}
}
