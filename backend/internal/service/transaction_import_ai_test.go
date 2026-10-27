package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
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

// TestCommitJob_ConcurrentCommitsRunOnce guards #649: a double-click or client
// retry can commit one staged job twice at once. Exactly one commit must run
// the import; the others are refused as not pending, and the job's audit row
// keeps the winner's counts instead of a loser's "0 imported".
func TestCommitJob_ConcurrentCommitsRunOnce(t *testing.T) {
	svc, userID, accountID, g := newAtomicImportSvc(t)
	ctx := context.Background()
	fx := &fakeExtractor{ext: &ingestion.Extraction{Source: "pdf", Rows: []ingestion.ParsedRow{
		ingestion.NewRow(1, "2026-05-15", "-4.50", "Coffee", 0.9),
		ingestion.NewRow(2, "2026-05-16", "2000.00", "Paycheck", 0.9),
		ingestion.NewRow(3, "2026-05-17", "-61.25", "Groceries", 0.9),
	}}}
	preview, err := svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), fx, nil)
	if err != nil {
		t.Fatalf("ExtractAndStage: %v", err)
	}
	jobID := *preview.JobID

	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CommitJob(ctx, userID, jobID)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var ok, refused int
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, service.ErrImportJobNotPending):
			refused++
		default:
			t.Fatalf("CommitJob: %v", err)
		}
	}
	if ok != 1 || refused != n-1 {
		t.Errorf("commits ok=%d refused=%d, want 1/%d", ok, refused, n-1)
	}
	if got := liveTxnCount(t, g, accountID); got != 3 {
		t.Errorf("live rows = %d, want 3", got)
	}
	job, err := repository.NewIngestionJobRepository(g).GetForUser(ctx, userID, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "completed" {
		t.Errorf("job status = %q, want completed", job.Status)
	}
	if job.RowsImported == nil || *job.RowsImported != 3 {
		t.Errorf("job rows_imported = %v, want 3 (a losing commit overwrote it)", derefInt(job.RowsImported))
	}
}

// TestCommitJob_FailedCommitCanRetry: a commit that fails after claiming the
// job hands it back as pending, so the user can retry from the same preview.
func TestCommitJob_FailedCommitCanRetry(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	fx := &fakeExtractor{ext: &ingestion.Extraction{Source: "pdf", Rows: []ingestion.ParsedRow{
		ingestion.NewRow(1, "2026-05-15", "-4.50", "Coffee", 0.9),
	}}}
	preview, err := svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), fx, nil)
	if err != nil {
		t.Fatalf("ExtractAndStage: %v", err)
	}
	// Corrupt the staged blob so the commit fails after its claim.
	if err := g.Model(&model.IngestionJob{}).Where("id = ?", *preview.JobID).
		Update("extraction", []byte(`"not an extraction"`)).Error; err != nil {
		t.Fatalf("corrupt job: %v", err)
	}
	if _, err := svc.CommitJob(ctx, userID, *preview.JobID); err == nil {
		t.Fatal("commit of a corrupt job succeeded")
	}
	job, err := repository.NewIngestionJobRepository(g).GetForUser(ctx, userID, *preview.JobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "extracted" {
		t.Errorf("job status after failed commit = %q, want extracted (retryable)", job.Status)
	}
	if got := liveTxnCount(t, g, accountID); got != 0 {
		t.Errorf("live rows = %d, want 0", got)
	}
}

// newAtomicImportSvc is newImportSvc with the production wiring: the DB handle
// that makes a commit one transaction (#651) and statement reconciliation.
func newAtomicImportSvc(t *testing.T) (*service.TransactionService, int64, int64, *gorm.DB) {
	t.Helper()
	svc, userID, accountID, g := newImportSvc(t)
	svc.WithStatementPositions(g, repository.NewAssetRepository(g))
	// Runs before newImportSvc's cleanups (LIFO): rows reference the job and
	// the position references the account, so clear them in FK order here.
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", accountID).Delete(&model.Transaction{})
		g.Where("account_id = ?", accountID).Delete(&model.Position{})
		g.Where("account_id = ?", accountID).Delete(&model.IngestionJob{})
	})
	return svc, userID, accountID, g
}

// rejectedPosition is a statement holding the assets table refuses (its kind
// fails the CHECK constraint), so the commit fails only AFTER its rows were
// inserted and the cash position refreshed — the torn-write window of #651.
func rejectedPosition() ingestion.ParsedPosition {
	return ingestion.ParsedPosition{
		Line: 9, Symbol: "QABAD651", AssetKind: "not-a-kind",
		Quantity: decimal.NewFromInt(1), AsOf: time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), Confidence: 1,
	}
}

func importRows() []ingestion.ParsedRow {
	return []ingestion.ParsedRow{
		ingestion.NewRow(1, "2026-05-15", "-4.50", "Coffee", 0.9),
		ingestion.NewRow(2, "2026-05-16", "2000.00", "Paycheck", 0.9),
		ingestion.NewRow(3, "2026-05-17", "-61.25", "Groceries", 0.9),
	}
}

func positionCount(t *testing.T, g *gorm.DB, accountID int64) int64 {
	t.Helper()
	var n int64
	if err := g.Model(&model.Position{}).Where("account_id = ? AND quantity <> 0", accountID).Count(&n).Error; err != nil {
		t.Fatalf("count positions: %v", err)
	}
	return n
}

// TestCommitJob_FailureAfterInsertRollsBack guards #651: a staged commit that
// fails after its rows were written must leave nothing behind — no rows, no
// position — and the job still "extracted", so a clean retry imports once.
func TestCommitJob_FailureAfterInsertRollsBack(t *testing.T) {
	svc, userID, accountID, g := newAtomicImportSvc(t)
	ctx := context.Background()
	fx := &fakeExtractor{ext: &ingestion.Extraction{
		Source:    "pdf",
		Rows:      importRows(),
		Positions: []ingestion.ParsedPosition{rejectedPosition()},
	}}
	preview, err := svc.ExtractAndStage(ctx, userID, accountID, pdfDoc(), fx, nil)
	if err != nil {
		t.Fatalf("ExtractAndStage: %v", err)
	}
	jobID := *preview.JobID

	if _, err := svc.CommitJob(ctx, userID, jobID); err == nil {
		t.Fatal("commit with a rejected position succeeded")
	}
	if got := liveTxnCount(t, g, accountID); got != 0 {
		t.Errorf("live rows after failed commit = %d, want 0 (torn import)", got)
	}
	if got := positionCount(t, g, accountID); got != 0 {
		t.Errorf("non-zero positions after failed commit = %d, want 0", got)
	}
	jobs := repository.NewIngestionJobRepository(g)
	job, err := jobs.GetForUser(ctx, userID, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "extracted" {
		t.Errorf("job status after failed commit = %q, want extracted (retryable)", job.Status)
	}

	// The fault clears (the staged statement no longer carries the bad
	// holding); the same job then commits exactly once.
	if err := g.Model(&model.IngestionJob{}).Where("id = ?", jobID).
		Update("extraction", mustJSON(t, ingestion.Extraction{Source: "pdf", Rows: importRows()})).Error; err != nil {
		t.Fatalf("restage job: %v", err)
	}
	res, err := svc.CommitJob(ctx, userID, jobID)
	if err != nil {
		t.Fatalf("retry CommitJob: %v", err)
	}
	if res.InsertedCount != 3 {
		t.Errorf("retry inserted = %d, want 3", res.InsertedCount)
	}
	if got := liveTxnCount(t, g, accountID); got != 3 {
		t.Errorf("live rows after retry = %d, want 3", got)
	}
	job, err = jobs.GetForUser(ctx, userID, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "completed" || job.RowsImported == nil || *job.RowsImported != 3 {
		t.Errorf("job after retry = %q rows_imported=%v, want completed/3", job.Status, derefInt(job.RowsImported))
	}
}

// TestImportDocument_FailureAfterInsertLeavesNothing: the direct (deterministic)
// commit path is all-or-nothing too (#651) — a failure after the rows insert
// leaves no rows and no orphan "processing"/"failed" job, and a re-upload then
// imports every row instead of reporting them all as duplicates.
func TestImportDocument_FailureAfterInsertLeavesNothing(t *testing.T) {
	svc, userID, accountID, g := newAtomicImportSvc(t)
	ctx := context.Background()
	doc := ingestion.Document{Filename: "may.csv", MIME: "text/csv", Data: []byte("may")}

	bad := &ingestion.Extraction{Source: "csv", Rows: importRows(), Positions: []ingestion.ParsedPosition{rejectedPosition()}}
	if _, err := svc.ImportDocument(ctx, userID, accountID, doc, bad, true); err == nil {
		t.Fatal("import with a rejected position succeeded")
	}
	if got := liveTxnCount(t, g, accountID); got != 0 {
		t.Errorf("live rows after failed import = %d, want 0 (torn import)", got)
	}
	if got := positionCount(t, g, accountID); got != 0 {
		t.Errorf("non-zero positions after failed import = %d, want 0", got)
	}
	var jobs int64
	if err := g.Model(&model.IngestionJob{}).Where("account_id = ?", accountID).Count(&jobs).Error; err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobs != 0 {
		t.Errorf("import jobs after failed import = %d, want 0 (orphan audit row)", jobs)
	}

	res, err := svc.ImportDocument(ctx, userID, accountID, doc, &ingestion.Extraction{Source: "csv", Rows: importRows()}, true)
	if err != nil {
		t.Fatalf("retry ImportDocument: %v", err)
	}
	if res.InsertedCount != 3 || res.DuplicateCount != 0 {
		t.Errorf("retry inserted=%d duplicates=%d, want 3/0", res.InsertedCount, res.DuplicateCount)
	}
	if got := liveTxnCount(t, g, accountID); got != 3 {
		t.Errorf("live rows after retry = %d, want 3", got)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func derefInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
