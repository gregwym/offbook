package model_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// TestPIIScan_FlagsLeak inserts a transaction whose description contains a
// known PII token and asserts the scanner flags it. Proves the scanner is
// doing real work.
func TestPIIScan_FlagsLeak(t *testing.T) {
	g := openTestDB(t)
	userID, acct := newTestAccount(t, g)

	const token = "Greg_PII_Token_" // distinctive so it can't collide with seed data
	desc := "Payment to " + token + "Smith"
	tx := model.Transaction{
		UserID:          userID,
		AccountID:       acct.ID,
		Amount:          decimal.RequireFromString("12.34"),
		Currency:        "USD",
		TransactionDate: time.Now().UTC().Truncate(24 * time.Hour),
		Source:          "manual",
		Description:     &desc,
	}
	if err := g.Create(&tx).Error; err != nil {
		t.Fatalf("insert tx: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })

	leaks, err := testutil.ScanForPIILeaks(t.Context(), g, []string{token}, testutil.DefaultPIIScanTargets())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var found bool
	for _, l := range leaks {
		if l.Table == "transactions" && l.Column == "description" && l.RowID == tx.ID && l.Token == token {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("scanner missed the planted leak; got: %+v", leaks)
	}
}

// TestPIIScan_PassesWhenClean stores a PII token only in pii_store (where
// it belongs) and asserts the scanner finds zero leaks across non-PII
// tables. Proves the scanner is correctly scoped — it must not flag
// pii_store itself.
func TestPIIScan_PassesWhenClean(t *testing.T) {
	g := openTestDB(t)
	_, acct := newTestAccount(t, g)

	const token = "Greg_PII_OnlyInStore_"
	rec := model.PIIRecord{
		EntityType: "account",
		EntityID:   acct.ID,
		FieldName:  "holder_name",
		Value:      token + "Smith",
	}
	if err := g.Create(&rec).Error; err != nil {
		t.Fatalf("insert pii_store: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.PIIRecord{}, rec.ID) })

	leaks, err := testutil.ScanForPIILeaks(t.Context(), g, []string{token}, testutil.DefaultPIIScanTargets())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(leaks) != 0 {
		t.Fatalf("expected zero leaks when token lives only in pii_store, got: %+v", leaks)
	}
}
