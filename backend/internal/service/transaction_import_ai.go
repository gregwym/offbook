package service

import (
	"context"
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

// AI-import domain errors (ADR-0019 §7). Handlers map each to an HTTP code.
var (
	ErrImportStagingUnavailable = errors.New("ai import staging not configured")
	ErrImportJobNotFound        = errors.New("import job not found")
	ErrImportJobNotPending      = errors.New("import job is not awaiting commit")
)

// ExtractAndStage runs a document extractor over a raw statement (the extractor
// re-validates every proposed row through ingestion's deterministic parsers),
// classifies the rows as a preview (writes no transactions), reconciles the row
// sum against any detected statement total, and STAGES the validated rows in an
// ingestion_jobs row. It returns the preview plus the staged JobID; the user
// reviews, then calls CommitJob — the extractor is never invoked twice
// (ADR-0019 §7). consentedAt records the user's consent to the (possibly cloud)
// egress and is non-nil whenever the extractor performed one.
func (s *TransactionService) ExtractAndStage(
	ctx context.Context,
	userID, accountID int64,
	doc ingestion.Document,
	extractor ingestion.Extractor,
	consentedAt *time.Time,
) (*ImportResult, error) {
	if s.jobRepo == nil {
		return nil, ErrImportStagingUnavailable
	}
	// Reject a foreign account before spending an extraction call.
	if _, err := s.fetchOwnedAccount(ctx, userID, accountID); err != nil {
		return nil, err
	}

	ext, err := extractor.Extract(ctx, doc)
	if err != nil {
		return nil, err
	}

	// Classify as a preview (commit=false → writes nothing). Reuses the exact
	// dedup/categorize/needs-review pipeline CSV import uses.
	preview, err := s.ImportStatement(ctx, userID, accountID, ext, false)
	if err != nil {
		return nil, err
	}
	reconcile(preview, ext)

	// Stage the validated extraction so commit applies it verbatim.
	staged, err := json.Marshal(ext)
	if err != nil {
		return nil, fmt.Errorf("stage extraction: %w", err)
	}
	providerName := extractor.Name()
	extractorKind := "ai"
	total := len(ext.Rows)
	job := &model.IngestionJob{
		UserID:      userID,
		Source:      ext.Source,
		Extractor:   &extractorKind,
		Provider:    &providerName,
		ConsentedAt: consentedAt,
		AccountID:   &accountID,
		Status:      "extracted",
		RowsTotal:   &total,
		Extraction:  staged,
	}
	if doc.Filename != "" {
		fn := doc.Filename
		job.FileName = &fn
	}
	if err := s.jobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("stage import job: %w", err)
	}
	preview.JobID = &job.ID
	return preview, nil
}

// CommitJob applies the rows staged by a prior ExtractAndStage. It re-validates
// and dedups exactly like any import, inserts the new rows, and marks the job
// completed. No second extractor call — deterministic and idempotent.
func (s *TransactionService) CommitJob(ctx context.Context, userID, jobID int64) (*ImportResult, error) {
	if s.jobRepo == nil {
		return nil, ErrImportStagingUnavailable
	}
	job, err := s.jobRepo.GetForUser(ctx, userID, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrImportJobNotFound
		}
		return nil, err
	}
	if job.Status != "extracted" {
		return nil, ErrImportJobNotPending
	}
	if job.AccountID == nil {
		return nil, fmt.Errorf("staged job %d has no account", jobID)
	}

	var ext ingestion.Extraction
	if err := json.Unmarshal(job.Extraction, &ext); err != nil {
		return nil, fmt.Errorf("rehydrate staged extraction: %w", err)
	}

	res, err := s.ImportStatement(ctx, userID, *job.AccountID, &ext, true)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	job.Status = "completed"
	job.RowsImported = &res.InsertedCount
	job.CompletedAt = &now
	if err := s.jobRepo.Update(ctx, job); err != nil {
		return nil, fmt.Errorf("finalize import job: %w", err)
	}
	res.JobID = &jobID
	return res, nil
}

// reconcile compares the signed sum of valid (importable) rows to any statement
// total the extractor detected, and records the result on the preview for the
// UI. With no detected total, Reconciled stays nil (not applicable) — we fall
// back to per-row confidence + mandatory review (ADR-0019 §3). A match (in
// magnitude, since totals' sign conventions vary) sets Reconciled true.
func reconcile(res *ImportResult, ext *ingestion.Extraction) {
	sum := decimal.Zero
	for _, r := range ext.Rows {
		if r.Valid() {
			sum = sum.Add(r.Amount)
		}
	}
	res.RowSum = sum.String()
	res.DocTotals = ext.DocTotals
	if len(ext.DocTotals) == 0 {
		return // not applicable
	}
	absSum := sum.Abs()
	matched := false
	for _, t := range ext.DocTotals {
		dt, err := decimal.NewFromString(strings.TrimSpace(t))
		if err != nil {
			continue
		}
		if dt.Abs().Equal(absSum) {
			matched = true
			break
		}
	}
	res.Reconciled = &matched
}
