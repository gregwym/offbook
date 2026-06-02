package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// fakeExtractor is an in-memory ingestion.Extractor: it returns a canned
// Extraction and counts calls, so tests can prove the extractor runs exactly
// once (extract, never re-run on commit).
type fakeExtractor struct {
	ext   *ingestion.Extraction
	err   error
	calls int
}

func (f *fakeExtractor) Extract(_ context.Context, _ ingestion.Document) (*ingestion.Extraction, error) {
	f.calls++
	return f.ext, f.err
}
func (f *fakeExtractor) Handles(string) bool { return true }
func (f *fakeExtractor) Name() string        { return "fake" }

func pdfDoc() ingestion.Document {
	return ingestion.Document{Filename: "stmt.pdf", MIME: "application/pdf", Data: []byte("%PDF")}
}

// TestExtractAndStage_ThenCommit walks the full AI import: extract (preview,
// writes nothing, stages a job), then commit by job id (inserts, idempotent),
// asserting the extractor is invoked exactly once.
func TestExtractAndStage_ThenCommit(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()

	fx := &fakeExtractor{ext: &ingestion.Extraction{
		Source:    "pdf",
		DocTotals: []string{"1995.50"},
		Rows: []ingestion.ParsedRow{
			ingestion.NewRow(1, "2026-05-15", "-4.50", "Coffee", 0.96),
			ingestion.NewRow(2, "2026-05-16", "2000.00", "Paycheck", 0.50), // < 0.8 → needs review
		},
	}}
	now := time.Now()

	preview, err := svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), fx, &now)
	if err != nil {
		t.Fatalf("ExtractAndStage: %v", err)
	}
	if preview.Committed {
		t.Error("preview marked committed")
	}
	if preview.JobID == nil {
		t.Fatal("preview has no JobID")
	}
	if preview.NewCount != 2 || preview.InsertedCount != 0 {
		t.Errorf("preview new=%d inserted=%d, want 2/0", preview.NewCount, preview.InsertedCount)
	}
	if preview.ReviewCount != 1 {
		t.Errorf("review_count = %d, want 1 (the 0.50-confidence row)", preview.ReviewCount)
	}
	if preview.Reconciled == nil || !*preview.Reconciled {
		t.Errorf("reconciled = %v, want true (|-4.50+2000|=1995.50)", preview.Reconciled)
	}
	if preview.RowSum != "1995.5" {
		t.Errorf("row_sum = %q, want 1995.5", preview.RowSum)
	}
	if n := liveTxnCount(t, g, accountID); n != 0 {
		t.Fatalf("preview persisted %d rows, want 0", n)
	}

	// Commit applies the staged rows; the extractor is NOT called again.
	res, err := svc.CommitJob(ctx, userID, *preview.JobID)
	if err != nil {
		t.Fatalf("CommitJob: %v", err)
	}
	if res.InsertedCount != 2 {
		t.Errorf("commit inserted=%d, want 2", res.InsertedCount)
	}
	if n := liveTxnCount(t, g, accountID); n != 2 {
		t.Errorf("after commit live=%d, want 2", n)
	}
	if fx.calls != 1 {
		t.Errorf("extractor called %d times, want exactly 1 (no re-run on commit)", fx.calls)
	}

	// Re-committing the same job is rejected (status is no longer 'extracted').
	if _, err := svc.CommitJob(ctx, userID, *preview.JobID); err != service.ErrImportJobNotPending {
		t.Errorf("double commit err = %v, want ErrImportJobNotPending", err)
	}
}

func TestExtractAndStage_RejectsForeignAccount(t *testing.T) {
	svc, _, accountID, _ := newImportSvc(t)
	fx := &fakeExtractor{ext: &ingestion.Extraction{Source: "pdf", Rows: []ingestion.ParsedRow{
		ingestion.NewRow(1, "2026-05-15", "-4.50", "Coffee", 0.9),
	}}}
	_, err := svc.ExtractAndStage(context.Background(), int64(-1), accountID, pdfDoc(), fx, nil)
	if err == nil {
		t.Fatal("expected error staging into a non-owned account")
	}
	if fx.calls != 0 {
		t.Errorf("extractor called %d times, want 0 (account check precedes extraction)", fx.calls)
	}
}

func TestCommitJob_ForeignUserCannotCommit(t *testing.T) {
	svc, userID, accountID, _ := newImportSvc(t)
	ctx := context.Background()
	fx := &fakeExtractor{ext: &ingestion.Extraction{Source: "pdf", Rows: []ingestion.ParsedRow{
		ingestion.NewRow(1, "2026-05-15", "-4.50", "Coffee", 0.9),
	}}}
	preview, err := svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), fx, nil)
	if err != nil {
		t.Fatalf("ExtractAndStage: %v", err)
	}
	// A different user must not commit another user's staged job.
	if _, err := svc.CommitJob(ctx, int64(-1), *preview.JobID); err != service.ErrImportJobNotFound {
		t.Errorf("cross-user commit err = %v, want ErrImportJobNotFound", err)
	}
}
