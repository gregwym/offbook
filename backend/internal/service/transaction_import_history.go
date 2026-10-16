package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// PriorImport identifies an earlier committed import of byte-identical
// document content into the same account (#376 duplicate-statement guard).
type PriorImport struct {
	JobID      int64      `json:"job_id"`
	FileName   string     `json:"file_name,omitempty"`
	ImportedAt *time.Time `json:"imported_at,omitempty"`
}

// DuplicateStatementError is returned by the AI extract path, before any
// provider egress, when the same document was already committed. Row-level
// external_id dedup remains the backstop if the caller overrides it.
type DuplicateStatementError struct{ Prior PriorImport }

func (e *DuplicateStatementError) Error() string {
	return "this statement was already imported into this account"
}

// ImportHistoryEntry is one committed import in an account's history — an
// append-only read of ingestion_jobs (no staged rows, no PII).
type ImportHistoryEntry struct {
	JobID         int64     `json:"job_id"`
	Source        string    `json:"source"`
	Extractor     string    `json:"extractor,omitempty"`
	FileName      string    `json:"file_name,omitempty"`
	ImportedAt    time.Time `json:"imported_at"`
	RowsTotal     int       `json:"rows_total"`
	RowsImported  int       `json:"rows_imported"`
	RowsDuplicate int       `json:"rows_duplicate"`
	RowsError     int       `json:"rows_error"`
	RowSum        string    `json:"row_sum,omitempty"`
	DocTotals     []string  `json:"doc_totals,omitempty"`
	// Reconciled is nil when the document had no total to compare. When false,
	// Difference is the smallest magnitude gap to any detected total.
	Reconciled  *bool      `json:"reconciled"`
	Difference  string     `json:"difference,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// contentHash is the hex SHA-256 of the uploaded document bytes.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// findPrior looks up an earlier committed import of the same bytes. Returns nil
// when there is none (or history is not wired).
func (s *TransactionService) findPrior(ctx context.Context, userID, accountID int64, hash string) (*PriorImport, error) {
	if s.jobRepo == nil || hash == "" {
		return nil, nil
	}
	j, err := s.jobRepo.FindCompletedByHash(ctx, userID, accountID, hash)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("duplicate-statement lookup: %w", err)
	}
	p := &PriorImport{JobID: j.ID, ImportedAt: j.CompletedAt}
	if p.ImportedAt == nil {
		t := j.CreatedAt
		p.ImportedAt = &t
	}
	if j.FileName != nil {
		p.FileName = *j.FileName
	}
	return p, nil
}

// ImportDocument is ImportStatement plus the #376 audit behaviour: it flags a
// byte-identical earlier import (DuplicateOf, non-blocking — row dedup still
// guarantees idempotence), surfaces statement-total reconciliation, and on
// commit records a completed ingestion_jobs row for the account's history.
func (s *TransactionService) ImportDocument(
	ctx context.Context,
	userID, accountID int64,
	doc ingestion.Document,
	ext *ingestion.Extraction,
	commit bool,
) (*ImportResult, error) {
	res, err := s.ImportStatement(ctx, userID, accountID, ext, commit)
	if err != nil {
		return nil, err
	}
	reconcile(res, ext)
	hash := contentHash(doc.Data)
	prior, err := s.findPrior(ctx, userID, accountID, hash)
	if err != nil {
		return nil, err
	}
	res.DuplicateOf = prior
	if !commit || s.jobRepo == nil {
		return res, nil
	}

	now := time.Now()
	extractorKind := "deterministic"
	source := ext.Source
	if source == "" {
		source = "csv"
	}
	job := &model.IngestionJob{
		UserID:      userID,
		Source:      source,
		Extractor:   &extractorKind,
		AccountID:   &accountID,
		Status:      "completed",
		CompletedAt: &now,
		ContentHash: &hash,
	}
	if doc.Filename != "" {
		fn := doc.Filename
		job.FileName = &fn
	}
	applyResultToJob(job, res)
	if err := s.jobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("record import job: %w", err)
	}
	res.JobID = &job.ID
	return res, nil
}

// applyResultToJob copies counts and reconciliation from an ImportResult onto
// the audit row.
func applyResultToJob(job *model.IngestionJob, res *ImportResult) {
	total, imported, dup, errs := res.TotalRows, res.InsertedCount, res.DuplicateCount, res.ErrorCount
	job.RowsTotal, job.RowsImported, job.RowsDuplicate, job.RowsError = &total, &imported, &dup, &errs
	job.Reconciled = res.Reconciled
	if d, err := decimal.NewFromString(res.RowSum); err == nil {
		job.RowSum = &d
	}
	if len(res.DocTotals) > 0 {
		if b, err := json.Marshal(res.DocTotals); err == nil {
			job.DocTotals = b
		}
	}
}

// ListImportHistory returns the account's committed imports, newest first.
func (s *TransactionService) ListImportHistory(ctx context.Context, userID, accountID int64) ([]ImportHistoryEntry, error) {
	if s.jobRepo == nil {
		return nil, ErrImportStagingUnavailable
	}
	if _, err := s.fetchOwnedAccount(ctx, userID, accountID); err != nil {
		return nil, err
	}
	jobs, err := s.jobRepo.ListCompletedByAccount(ctx, userID, accountID, 50)
	if err != nil {
		return nil, err
	}
	out := make([]ImportHistoryEntry, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, historyEntry(j))
	}
	return out, nil
}

func historyEntry(j model.IngestionJob) ImportHistoryEntry {
	e := ImportHistoryEntry{
		JobID: j.ID, Source: j.Source, ImportedAt: j.CreatedAt,
		Reconciled: j.Reconciled, CompletedAt: j.CompletedAt,
	}
	if j.CompletedAt != nil {
		e.ImportedAt = *j.CompletedAt
	}
	if j.Extractor != nil {
		e.Extractor = *j.Extractor
	}
	if j.FileName != nil {
		e.FileName = *j.FileName
	}
	if j.RowsTotal != nil {
		e.RowsTotal = *j.RowsTotal
	}
	if j.RowsImported != nil {
		e.RowsImported = *j.RowsImported
	}
	if j.RowsDuplicate != nil {
		e.RowsDuplicate = *j.RowsDuplicate
	}
	if j.RowsError != nil {
		e.RowsError = *j.RowsError
	}
	if j.RowSum != nil {
		e.RowSum = j.RowSum.String()
	}
	if len(j.DocTotals) > 0 {
		_ = json.Unmarshal(j.DocTotals, &e.DocTotals)
	}
	if j.Reconciled != nil && !*j.Reconciled && j.RowSum != nil {
		e.Difference = totalsDifference(j.RowSum.Abs(), e.DocTotals)
	}
	return e
}

// totalsDifference is the smallest |sum| gap to any parsable detected total
// (sign conventions vary, so magnitudes are compared, as reconcile does).
func totalsDifference(absSum decimal.Decimal, totals []string) string {
	var best *decimal.Decimal
	for _, t := range totals {
		dt, err := decimal.NewFromString(strings.TrimSpace(t))
		if err != nil {
			continue
		}
		d := dt.Abs().Sub(absSum).Abs()
		if best == nil || d.LessThan(*best) {
			best = &d
		}
	}
	if best == nil {
		return ""
	}
	return best.String()
}
