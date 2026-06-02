package service_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// newImportSvc builds a TransactionService plus an account whose primary quote
// asset is USD, so imported cash rows satisfy the NOT NULL asset_id FK.
func newImportSvc(t *testing.T) (svc *service.TransactionService, userID, accountID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	txRepo := repository.NewTransactionRepository(g)
	accRepo := repository.NewAccountRepository(g)
	catRepo := repository.NewCategoryRepository(g)
	svc = service.NewTransactionService(txRepo, accRepo, catRepo)

	userID = seedTestUser(t, g)
	usdID := testutil.LookupUSDAssetID(t, g)
	acc := &model.Account{
		UserID:              userID,
		Name:                "import-" + time.Now().Format("150405.000000000"),
		InstitutionSlug:     "fixture",
		AccountType:         "checking",
		PrimaryQuoteAssetID: usdID,
		IsActive:            true,
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	// Cleanup runs LIFO: register the account delete first so it runs LAST,
	// after the transactions referencing it are hard-deleted (FK order).
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, acc.ID) })
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{}) })
	return svc, userID, acc.ID, g
}

// mustExtract runs the fixture CSV through the CSV extractor, producing the
// neutral Extraction that ImportStatement consumes.
func mustExtract(t *testing.T, csv string) *ingestion.Extraction {
	t.Helper()
	doc := ingestion.Document{MIME: "text/csv", Data: []byte(csv)}
	ext, err := ingestion.CSVExtractor{}.Extract(context.Background(), doc)
	if err != nil {
		t.Fatalf("extract fixture: %v", err)
	}
	return ext
}

func liveTxnCount(t *testing.T, g *gorm.DB, accountID int64) int64 {
	t.Helper()
	var n int64
	if err := g.Model(&model.Transaction{}).Where("account_id = ?", accountID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestImportStatement_PreviewWritesNothing(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	csv := "Date,Description,Amount\n2026-05-15,Coffee,-4.50\n2026-05-16,Paycheck,2000.00\n"

	res, err := svc.ImportStatement(ctx, userID, accountID, mustExtract(t, csv), false)
	if err != nil {
		t.Fatalf("ImportStatement preview: %v", err)
	}
	if res.Committed {
		t.Error("preview marked committed")
	}
	if res.NewCount != 2 || res.DuplicateCount != 0 || res.ErrorCount != 0 {
		t.Errorf("counts = new %d dup %d err %d, want 2/0/0", res.NewCount, res.DuplicateCount, res.ErrorCount)
	}
	if res.InsertedCount != 0 {
		t.Errorf("preview inserted %d rows, want 0", res.InsertedCount)
	}
	// Deterministic CSV rows are fully trusted: confidence 1.0, never flagged.
	if res.ReviewCount != 0 {
		t.Errorf("review_count = %d, want 0 for deterministic CSV", res.ReviewCount)
	}
	if res.Source != "csv" {
		t.Errorf("source = %q, want csv", res.Source)
	}
	for _, r := range res.Rows {
		if r.Confidence != 1.0 || r.NeedsReview {
			t.Errorf("row line %d: confidence=%v needs_review=%v, want 1.0/false", r.Line, r.Confidence, r.NeedsReview)
		}
	}
	if n := liveTxnCount(t, g, accountID); n != 0 {
		t.Errorf("preview persisted %d rows, want 0", n)
	}
}

func TestImportStatement_CommitThenReimportIsIdempotent(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	csv := "Date,Description,Amount\n2026-05-15,Coffee,-4.50\n2026-05-16,Paycheck,2000.00\n"

	first, err := svc.ImportStatement(ctx, userID, accountID, mustExtract(t, csv), true)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if first.NewCount != 2 || first.InsertedCount != 2 {
		t.Fatalf("first import new=%d inserted=%d, want 2/2", first.NewCount, first.InsertedCount)
	}
	if n := liveTxnCount(t, g, accountID); n != 2 {
		t.Fatalf("after first import live=%d, want 2", n)
	}

	// Re-import the identical file: every row should now be a duplicate and
	// nothing new is written.
	second, err := svc.ImportStatement(ctx, userID, accountID, mustExtract(t, csv), true)
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if second.NewCount != 0 || second.DuplicateCount != 2 || second.InsertedCount != 0 {
		t.Errorf("re-import new=%d dup=%d inserted=%d, want 0/2/0",
			second.NewCount, second.DuplicateCount, second.InsertedCount)
	}
	if n := liveTxnCount(t, g, accountID); n != 2 {
		t.Errorf("after re-import live=%d, want 2 (no dupes)", n)
	}
}

func TestImportStatement_IdenticalRowsInFileBothSurvive(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	// Two genuinely identical lines (e.g. two $4.50 coffees same day) must both
	// import — the per-file occurrence index disambiguates their external_ids.
	csv := "Date,Description,Amount\n2026-05-15,Coffee,-4.50\n2026-05-15,Coffee,-4.50\n"

	res, err := svc.ImportStatement(ctx, userID, accountID, mustExtract(t, csv), true)
	if err != nil {
		t.Fatalf("ImportStatement: %v", err)
	}
	if res.NewCount != 2 || res.InsertedCount != 2 {
		t.Errorf("new=%d inserted=%d, want 2/2 (identical rows both kept)", res.NewCount, res.InsertedCount)
	}
	if n := liveTxnCount(t, g, accountID); n != 2 {
		t.Errorf("live=%d, want 2", n)
	}

	// And re-importing that same two-line file stays idempotent.
	again, err := svc.ImportStatement(ctx, userID, accountID, mustExtract(t, csv), true)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if again.InsertedCount != 0 || again.DuplicateCount != 2 {
		t.Errorf("re-import inserted=%d dup=%d, want 0/2", again.InsertedCount, again.DuplicateCount)
	}
}

func TestImportStatement_ErrorRowsSkipped(t *testing.T) {
	svc, userID, accountID, g := newImportSvc(t)
	ctx := context.Background()
	csv := "Date,Description,Amount\nnot-a-date,Bad,-1.00\n2026-05-16,Good,-2.00\n"

	res, err := svc.ImportStatement(ctx, userID, accountID, mustExtract(t, csv), true)
	if err != nil {
		t.Fatalf("ImportStatement: %v", err)
	}
	if res.ErrorCount != 1 || res.NewCount != 1 || res.InsertedCount != 1 {
		t.Errorf("err=%d new=%d inserted=%d, want 1/1/1", res.ErrorCount, res.NewCount, res.InsertedCount)
	}
	if n := liveTxnCount(t, g, accountID); n != 1 {
		t.Errorf("live=%d, want 1 (error row skipped)", n)
	}
}

func TestImportStatement_RejectsForeignAccount(t *testing.T) {
	svc, _, accountID, _ := newImportSvc(t)
	ctx := context.Background()
	csv := "Date,Description,Amount\n2026-05-15,Coffee,-4.50\n"

	// A different user importing into the first user's account must be rejected.
	otherUser := int64(-1) // no such user owns accountID
	_, err := svc.ImportStatement(ctx, otherUser, accountID, mustExtract(t, csv), true)
	if err == nil {
		t.Fatal("expected error importing into a non-owned account, got nil")
	}
}
