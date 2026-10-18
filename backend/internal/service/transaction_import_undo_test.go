package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

func stmtExtraction(closing string) *ingestion.Extraction {
	bal, at, _ := ingestion.ParseClosingBalance(closing, "2026-05-31")
	return &ingestion.Extraction{
		Source: "pdf",
		Rows: []ingestion.ParsedRow{
			ingestion.NewRow(1, "2026-05-10", "-100.00", "Rent", 1),
			ingestion.NewRow(2, "2026-05-11", "-50.00", "Gas", 1),
		},
		ClosingBalance:   &bal,
		ClosingBalanceAt: &at,
	}
}

// TestUndoImport_RestoresPreImportBalance is the #483 repro: fold 1000, import
// rows −100/−50 with closing balance 900 (adjustment +50), undo → 1000 again,
// observation and adjustment gone, position re-folded, re-import idempotent.
func TestUndoImport_RestoresPreImportBalance(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	svc = svc.WithStatementPositions(g, repository.NewAssetRepository(g))
	ctx := context.Background()
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Position{}) })
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Transaction{}) })
	t.Cleanup(func() { g.Where("account_id = ?", accountID).Delete(&model.AccountBalanceObservation{}) })

	var acct model.Account
	if err := g.First(&acct, accountID).Error; err != nil {
		t.Fatal(err)
	}
	desc := "seed"
	seed := model.Transaction{
		UserID: userID, AccountID: accountID, AssetID: acct.PrimaryQuoteAssetID, Kind: model.KindOpeningBalance,
		Amount: decimal.NewFromInt(1000), Description: &desc, TransactionDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Source: "system",
	}
	if err := g.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}
	txRepo := repository.NewTransactionRepository(g)
	fold := func() decimal.Decimal {
		q, err := txRepo.FoldQuantity(ctx, userID, accountID, acct.PrimaryQuoteAssetID)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	posQty := func() decimal.Decimal {
		var p model.Position
		if err := g.Where("account_id = ? AND asset_id = ?", accountID, acct.PrimaryQuoteAssetID).First(&p).Error; err != nil {
			return decimal.Zero
		}
		return p.Quantity
	}
	doc := csvDoc("may.pdf", "statement-bytes")

	res, err := svc.ImportDocument(ctx, userID, accountID, doc, stmtExtraction("900.00"), true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.AdjustmentsWritten != 1 || !fold().Equal(decimal.NewFromInt(900)) || !posQty().Equal(decimal.NewFromInt(900)) {
		t.Fatalf("after import adj=%d fold=%s pos=%s, want 1/900/900", res.AdjustmentsWritten, fold(), posQty())
	}
	var tagged int64
	g.Model(&model.Transaction{}).Where("import_job_id = ?", *res.JobID).Count(&tagged)
	if tagged != 3 { // two flow rows + the adjustment
		t.Errorf("rows tagged with job = %d, want 3 (set at insert, incl. adjustment)", tagged)
	}

	if _, err := svc.UndoImport(ctx, userID, accountID, *res.JobID); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !fold().Equal(decimal.NewFromInt(1000)) || !posQty().Equal(decimal.NewFromInt(1000)) {
		t.Errorf("after undo fold=%s pos=%s, want 1000/1000 (pre-import state)", fold(), posQty())
	}
	var obs int64
	g.Model(&model.AccountBalanceObservation{}).Where("account_id = ?", accountID).Count(&obs)
	if obs != 0 {
		t.Errorf("observations after undo = %d, want 0", obs)
	}

	// Re-import is idempotent in effect: same end state as the first import.
	res2, err := svc.ImportDocument(ctx, userID, accountID, doc, stmtExtraction("900.00"), true)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res2.InsertedCount != 2 || !fold().Equal(decimal.NewFromInt(900)) {
		t.Errorf("re-import inserted=%d fold=%s, want 2/900", res2.InsertedCount, fold())
	}
}

// TestUndoImport_RevertsStatementPositions covers ParsedPosition adjustments:
// undo returns a statement-created holding to its pre-import quantity (0).
func TestUndoImport_RevertsStatementPositions(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	svc = svc.WithStatementPositions(g, repository.NewAssetRepository(g))
	ctx := context.Background()
	symbol := fmt.Sprintf("U%d", time.Now().UnixNano()%1_000_000_000)
	t.Cleanup(func() { g.Unscoped().Where("symbol IN ?", []string{symbol, symbol + "X"}).Delete(&model.Asset{}) })
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Position{}) })
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Transaction{}) })
	t.Cleanup(func() { g.Where("account_id = ?", accountID).Delete(&model.AccountBalanceObservation{}) })

	res, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("b.pdf", "brokerage-bytes"), brokerageExtraction(symbol, "12.5", "1100.00"), true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var asset model.Asset
	if err := g.Where("symbol = ?", symbol).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	txRepo := repository.NewTransactionRepository(g)
	q, _ := txRepo.FoldQuantity(ctx, userID, accountID, asset.ID)
	if !q.Equal(decimal.RequireFromString("12.5")) {
		t.Fatalf("holding fold = %s, want 12.5", q)
	}
	if _, err := svc.UndoImport(ctx, userID, accountID, *res.JobID); err != nil {
		t.Fatalf("undo: %v", err)
	}
	q, _ = txRepo.FoldQuantity(ctx, userID, accountID, asset.ID)
	var p model.Position
	g.Where("account_id = ? AND asset_id = ?", accountID, asset.ID).First(&p)
	if !q.IsZero() || !p.Quantity.IsZero() {
		t.Errorf("after undo holding fold=%s position=%s, want 0/0", q, p.Quantity)
	}
	var live int64
	g.Model(&model.Transaction{}).Where("account_id = ?", accountID).Count(&live)
	if live != 0 {
		t.Errorf("live rows after undo = %d, want 0", live)
	}
}
