package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

const historyCSV = "Date,Amount,Description\n2026-05-15,-4.50,Coffee\n2026-05-16,2000.00,Paycheck\n"

func csvDoc(name, csv string) ingestion.Document {
	return ingestion.Document{Filename: name, MIME: "text/csv", Data: []byte(csv)}
}

func TestImportDocument_PreviewWritesNoHistory(t *testing.T) {
	svc, userID, accountID, _ := newImportSvc(t)
	ctx := context.Background()
	if _, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("may.csv", historyCSV), mustExtract(t, historyCSV), false); err != nil {
		t.Fatalf("preview: %v", err)
	}
	h, err := svc.ListImportHistory(ctx, userID, accountID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(h) != 0 {
		t.Fatalf("preview recorded %d history rows, want 0", len(h))
	}
}

func TestImportDocument_CommitRecordsHistoryAndFlagsReupload(t *testing.T) {
	svc, userID, accountID, _ := newImportSvc(t)
	ctx := context.Background()
	doc := csvDoc("may.csv", historyCSV)

	res, err := svc.ImportDocument(ctx, userID, accountID, doc, mustExtract(t, historyCSV), true)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.DuplicateOf != nil {
		t.Errorf("first import flagged as duplicate: %+v", res.DuplicateOf)
	}
	h, err := svc.ListImportHistory(ctx, userID, accountID)
	if err != nil || len(h) != 1 {
		t.Fatalf("history len=%d err=%v, want 1", len(h), err)
	}
	e := h[0]
	if e.FileName != "may.csv" || e.RowsImported != 2 || e.RowsDuplicate != 0 || e.RowsTotal != 2 {
		t.Errorf("entry = %+v", e)
	}
	if e.Reconciled != nil {
		t.Errorf("csv has no statement total; reconciled = %v, want nil", *e.Reconciled)
	}
	if e.RowSum != "1995.5" {
		t.Errorf("row_sum = %q, want 1995.5", e.RowSum)
	}

	// Same bytes again: preview warns with the prior import; commit adds a
	// second history row showing 2 duplicates, zero new (row dedup backstop).
	pre, err := svc.ImportDocument(ctx, userID, accountID, doc, mustExtract(t, historyCSV), false)
	if err != nil {
		t.Fatalf("re-preview: %v", err)
	}
	if pre.DuplicateOf == nil || pre.DuplicateOf.JobID != e.JobID || pre.DuplicateOf.FileName != "may.csv" {
		t.Errorf("duplicate_of = %+v, want prior job %d", pre.DuplicateOf, e.JobID)
	}
	if _, err := svc.ImportDocument(ctx, userID, accountID, doc, mustExtract(t, historyCSV), true); err != nil {
		t.Fatalf("re-commit: %v", err)
	}
	h, _ = svc.ListImportHistory(ctx, userID, accountID)
	if len(h) != 2 || h[0].RowsImported != 0 || h[0].RowsDuplicate != 2 {
		t.Errorf("history after re-commit = %+v", h)
	}
}

func TestListImportHistory_RejectsForeignAccount(t *testing.T) {
	svc, _, accountID, g := newImportSvc(t)
	other := seedTestUser(t, g)
	if _, err := svc.ListImportHistory(context.Background(), other, accountID); !errors.Is(err, service.ErrAccountNotFound) {
		t.Fatalf("foreign history err = %v, want ErrAccountNotFound", err)
	}
}

func TestExtractAndStage_DuplicateBlockedBeforeEgress(t *testing.T) {
	svc, userID, accountID, _ := newImportSvc(t)
	ctx := context.Background()
	now := time.Now()
	mk := func() *fakeExtractor {
		return &fakeExtractor{ext: &ingestion.Extraction{
			Source:    "pdf",
			DocTotals: []string{"100.00"},
			Rows:      []ingestion.ParsedRow{ingestion.NewRow(1, "2026-05-15", "-4.50", "Coffee", 0.96)},
		}}
	}

	fx := mk()
	preview, err := svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), fx, &now)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	// Staged-but-uncommitted jobs are not "already imported".
	if _, err := svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), mk(), &now); err != nil {
		t.Fatalf("restage before commit should be allowed: %v", err)
	}
	if _, err := svc.CommitJob(ctx, userID, *preview.JobID); err != nil {
		t.Fatalf("commit: %v", err)
	}

	fx2 := mk()
	_, err = svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), fx2, &now)
	var dup *service.DuplicateStatementError
	if !errors.As(err, &dup) || dup.Prior.JobID != *preview.JobID {
		t.Fatalf("err = %v, want DuplicateStatementError for job %d", err, *preview.JobID)
	}
	if fx2.calls != 0 {
		t.Errorf("extractor called %d times for a duplicate; want 0 (no egress)", fx2.calls)
	}

	fx3 := mk()
	again, err := svc.ExtractAndStageWith(ctx, userID, accountID, pdfDoc(), fx3, &now, true)
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if fx3.calls != 1 || again.DuplicateOf == nil {
		t.Errorf("override calls=%d duplicate_of=%v", fx3.calls, again.DuplicateOf)
	}

	// Committed history records the doc-total mismatch (|-4.50| != 100.00).
	h, _ := svc.ListImportHistory(ctx, userID, accountID)
	if len(h) != 1 || h[0].Reconciled == nil || *h[0].Reconciled || h[0].Difference != "95.5" {
		t.Errorf("history = %+v, want 1 entry mismatch diff 95.5", h)
	}
}

func TestUndoImport_RemovesOnlyThatImportsRowsAndAllowsReimport(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	const csvA = "Date,Amount,Description\n2026-05-15,-4.50,Coffee\n2026-05-16,2000.00,Paycheck\n"
	const csvB = "Date,Amount,Description\n2026-06-01,-10.00,Lunch\n"

	if _, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("a.csv", csvA), mustExtract(t, csvA), true); err != nil {
		t.Fatalf("import A: %v", err)
	}
	if _, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("b.csv", csvB), mustExtract(t, csvB), true); err != nil {
		t.Fatalf("import B: %v", err)
	}
	h, err := svc.ListImportHistory(ctx, userID, accountID)
	if err != nil || len(h) != 2 || !h[0].CanUndo || !h[1].CanUndo {
		t.Fatalf("history = %+v err=%v, want 2 undoable entries", h, err)
	}
	jobA := h[1].JobID

	entry, err := svc.UndoImport(ctx, userID, accountID, jobA)
	if err != nil {
		t.Fatalf("undo A: %v", err)
	}
	if entry.UndoneAt == nil || entry.RowsUndone != 2 || entry.CanUndo {
		t.Errorf("undo entry = %+v, want undone with 2 rows removed", entry)
	}
	var live int64
	g.Model(&model.Transaction{}).Where("account_id = ?", accountID).Count(&live)
	if live != 1 {
		t.Errorf("live rows after undo = %d, want 1 (import B untouched)", live)
	}

	// Second undo is refused; the duplicate-statement guard no longer sees A.
	if _, err := svc.UndoImport(ctx, userID, accountID, jobA); !errors.Is(err, service.ErrImportNotUndoable) {
		t.Errorf("repeat undo err = %v, want ErrImportNotUndoable", err)
	}
	pre, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("a.csv", csvA), mustExtract(t, csvA), false)
	if err != nil || pre.DuplicateOf != nil || pre.NewCount != 2 {
		t.Errorf("preview after undo: dup=%v new=%d err=%v, want no duplicate and 2 new", pre.DuplicateOf, pre.NewCount, err)
	}
	re, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("a.csv", csvA), mustExtract(t, csvA), true)
	if err != nil || re.InsertedCount != 2 {
		t.Fatalf("re-import inserted=%v err=%v, want 2", re, err)
	}
}

func TestUndoImport_RejectsForeignUserAndAccount(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	if _, err := svc.ImportDocument(ctx, userID, accountID, csvDoc("a.csv", historyCSV), mustExtract(t, historyCSV), true); err != nil {
		t.Fatalf("import: %v", err)
	}
	h, _ := svc.ListImportHistory(ctx, userID, accountID)
	other := seedTestUser(t, g)
	if _, err := svc.UndoImport(ctx, other, accountID, h[0].JobID); !errors.Is(err, service.ErrAccountNotFound) {
		t.Errorf("foreign user err = %v, want ErrAccountNotFound", err)
	}
	var live int64
	g.Model(&model.Transaction{}).Where("account_id = ?", accountID).Count(&live)
	if live != 2 {
		t.Errorf("rows after rejected undo = %d, want 2", live)
	}
}
