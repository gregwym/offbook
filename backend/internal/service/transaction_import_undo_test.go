package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
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

// TestUndoImport_UnlinksTransferPartner is the #488 repro: undoing an import
// whose row was matched as a transfer must release the surviving partner so
// it counts as ordinary spending/income again. Single delete behaves alike (#484).
func TestUndoImport_UnlinksTransferPartner(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Transaction{}) })

	var acct model.Account
	if err := g.First(&acct, accountID).Error; err != nil {
		t.Fatal(err)
	}
	mkPartner := func(desc string) *model.Transaction {
		p := &model.Transaction{
			UserID: userID, AccountID: accountID, AssetID: acct.PrimaryQuoteAssetID, Kind: model.KindFlow,
			Amount: decimal.NewFromInt(100), Description: &desc,
			TransactionDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), Source: "manual",
		}
		if err := g.Create(p).Error; err != nil {
			t.Fatal(err)
		}
		return p
	}
	assertFree := func(label string, id int64) {
		var p model.Transaction
		if err := g.First(&p, id).Error; err != nil {
			t.Fatal(err)
		}
		if p.IsTransfer || p.TransferPairID != nil {
			t.Errorf("%s: partner is_transfer=%v pair=%v, want unlinked", label, p.IsTransfer, p.TransferPairID)
		}
	}

	res, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("t.pdf", "transfer-bytes"), stmtExtraction("900.00"), true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var rent model.Transaction
	if err := g.Where("import_job_id = ? AND amount = ?", *res.JobID, decimal.NewFromInt(-100)).First(&rent).Error; err != nil {
		t.Fatal(err)
	}
	partner := mkPartner("partner-undo")
	txRepo := repository.NewTransactionRepository(g)
	if _, _, err := txRepo.LinkTransferPair(ctx, userID, rent.ID, partner.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, err := svc.UndoImport(ctx, userID, accountID, *res.JobID); err != nil {
		t.Fatalf("undo: %v", err)
	}
	assertFree("import undo", partner.ID)

	// Single delete of one leg releases the other.
	a, b := mkPartner("leg-a"), mkPartner("leg-b")
	if _, _, err := txRepo.LinkTransferPair(ctx, userID, a.ID, b.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := svc.SoftDelete(ctx, userID, a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	assertFree("single delete", b.ID)
}

// TestUndoImport_BlocksOlderWhileNewerStands is the #489 repro: undoing import
// #1 while import #2 stands is refused; unwinding newest-first works.
func TestUndoImport_BlocksOlderWhileNewerStands(t *testing.T) {
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
	if err := g.Create(&model.Transaction{
		UserID: userID, AccountID: accountID, AssetID: acct.PrimaryQuoteAssetID, Kind: model.KindOpeningBalance,
		Amount: decimal.NewFromInt(1000), Description: &desc, TransactionDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Source: "system",
	}).Error; err != nil {
		t.Fatal(err)
	}
	res1, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("may.pdf", "may-bytes"), stmtExtraction("900.00"), true)
	if err != nil {
		t.Fatalf("import 1: %v", err)
	}
	bal, at, _ := ingestion.ParseClosingBalance("880.00", "2026-06-30")
	ex2 := &ingestion.Extraction{
		Source:           "pdf",
		Rows:             []ingestion.ParsedRow{ingestion.NewRow(1, "2026-06-10", "-20.00", "Water", 1)},
		ClosingBalance:   &bal,
		ClosingBalanceAt: &at,
	}
	res2, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("jun.pdf", "jun-bytes"), ex2, true)
	if err != nil {
		t.Fatalf("import 2: %v", err)
	}
	// History says so before the user tries (#522): the older import names the
	// newer one as its blocker; the newer one is undoable.
	assertHistory := func(label string, wantOlderBlockedBy *int64, wantOlderCanUndo bool) {
		t.Helper()
		h, err := svc.ListImportHistory(ctx, userID, accountID)
		if err != nil || len(h) != 2 {
			t.Fatalf("%s: history len=%d err=%v", label, len(h), err)
		}
		older := h[1]
		if older.JobID != *res1.JobID {
			t.Fatalf("%s: history order = %d,%d", label, h[0].JobID, h[1].JobID)
		}
		if older.CanUndo != wantOlderCanUndo {
			t.Errorf("%s: older can_undo = %v, want %v", label, older.CanUndo, wantOlderCanUndo)
		}
		switch {
		case wantOlderBlockedBy == nil && older.UndoBlockedBy != nil:
			t.Errorf("%s: older undo_blocked_by = %d, want none", label, *older.UndoBlockedBy)
		case wantOlderBlockedBy != nil && (older.UndoBlockedBy == nil || *older.UndoBlockedBy != *wantOlderBlockedBy):
			t.Errorf("%s: older undo_blocked_by = %v, want %d", label, older.UndoBlockedBy, *wantOlderBlockedBy)
		}
		if h[0].UndoBlockedBy != nil {
			t.Errorf("%s: newest import reported blocked by %d", label, *h[0].UndoBlockedBy)
		}
		if h[0].UndoneAt == nil && !h[0].CanUndo {
			t.Errorf("%s: newest standing import should be undoable", label)
		}
	}
	assertHistory("both standing", res2.JobID, false)

	if _, err := svc.UndoImport(ctx, userID, accountID, *res1.JobID); !errors.Is(err, service.ErrImportNotUndoable) {
		t.Fatalf("undo older err = %v, want ErrImportNotUndoable", err)
	}
	if _, err := svc.UndoImport(ctx, userID, accountID, *res2.JobID); err != nil {
		t.Fatalf("undo newest: %v", err)
	}
	assertHistory("newest undone", nil, true)
	if _, err := svc.UndoImport(ctx, userID, accountID, *res1.JobID); err != nil {
		t.Fatalf("undo older after newest: %v", err)
	}
}
