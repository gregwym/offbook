package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// ImportRowStatus classifies a parsed CSV row in the import preview/result.
type ImportRowStatus string

const (
	ImportNew       ImportRowStatus = "new"       // will be (or was) inserted
	ImportDuplicate ImportRowStatus = "duplicate" // already present, skipped
	ImportError     ImportRowStatus = "error"     // failed to parse, skipped
)

// reviewConfidenceThreshold gates "needs review": a new row whose extractor
// confidence is below this is flagged for the user to confirm before commit.
// Deterministic CSV rows are confidence 1.0, so they never trip it — the gate
// only bites once the AI extractor (ADR-0019 phase 2) reports real confidence.
const reviewConfidenceThreshold = 0.8

// ImportRowResult is the per-row outcome surfaced to the UI for both preview
// (commit=false) and commit (commit=true) runs.
type ImportRowResult struct {
	Line        int             `json:"line"`
	Date        time.Time       `json:"date"`
	Amount      decimal.Decimal `json:"amount"`
	Description string          `json:"description"`
	ExternalID  string          `json:"external_id,omitempty"`
	Status      ImportRowStatus `json:"status"`
	Error       string          `json:"error,omitempty"`
	// Confidence is the extractor's certainty (0–1) for this row. NeedsReview is
	// set when a new row's confidence is below reviewConfidenceThreshold; the UI
	// surfaces these and blocks one-click commit until acknowledged.
	Confidence  float64 `json:"confidence"`
	NeedsReview bool    `json:"needs_review"`
}

// ImportResult summarizes a statement import attempt. On a preview run nothing
// is written and InsertedCount is 0; the counts let the UI render "N new, M
// duplicates, K errors" before the user commits. ReviewCount is how many new
// rows need review.
type ImportResult struct {
	Committed      bool                    `json:"committed"`
	Source         string                  `json:"source"`
	Mapping        ingestion.ColumnMapping `json:"mapping"`
	Headers        []string                `json:"headers"`
	TotalRows      int                     `json:"total_rows"`
	NewCount       int                     `json:"new_count"`
	DuplicateCount int                     `json:"duplicate_count"`
	ErrorCount     int                     `json:"error_count"`
	ReviewCount    int                     `json:"review_count"`
	InsertedCount  int                     `json:"inserted_count"`
	Rows           []ImportRowResult       `json:"rows"`
}

// ImportStatement classifies extracted statement rows against the target
// account and, when commit is true, inserts the new ones. Transactions are
// deduped on a content-derived external_id (date|amount|description +
// per-file occurrence index, prefixed by source) so re-importing the same
// file is idempotent, while two genuinely identical lines in one file both
// survive. The source ("csv", and pdf/photo in ADR-0019 phase 2) comes from
// the extraction; uncategorized rows are run through the user's categorization
// rules on insert.
func (s *TransactionService) ImportStatement(ctx context.Context, userID, accountID int64, ext *ingestion.Extraction, commit bool) (*ImportResult, error) {
	// fetchOwnedAccount rejects another user's account and gives us the asset.
	account, err := s.fetchOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}

	source := ext.Source
	if source == "" {
		source = "csv" // defensive: every extractor sets this
	}

	// Pass 1: derive a stable external_id per valid row.
	type computed struct {
		row ingestion.ParsedRow
		eid string // empty for invalid rows
	}
	occ := map[string]int{}
	rows := make([]computed, 0, len(ext.Rows))
	validIDs := make([]string, 0, len(ext.Rows))
	for _, row := range ext.Rows {
		c := computed{row: row}
		if row.Valid() {
			base := dedupKey(row)
			n := occ[base]
			occ[base]++
			c.eid = importExternalID(source, base, n)
			validIDs = append(validIDs, c.eid)
		}
		rows = append(rows, c)
	}

	existing, err := s.repo.ExistingExternalIDs(ctx, userID, accountID, validIDs)
	if err != nil {
		return nil, fmt.Errorf("dedup lookup: %w", err)
	}

	// Compile the user's rules once for the whole file.
	var compiled []categorization.CompiledRule
	if s.ruleRepo != nil {
		rules, err := s.ruleRepo.List(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("load rules: %w", err)
		}
		compiled = categorization.Compile(rules)
	}

	res := &ImportResult{
		Committed: commit,
		Source:    source,
		Mapping:   ext.Mapping,
		Headers:   ext.Headers,
		TotalRows: len(ext.Rows),
	}
	var toInsert []model.Transaction
	seen := map[string]struct{}{} // guards in-file dup external_ids

	for _, c := range rows {
		rr := ImportRowResult{
			Line:        c.row.Line,
			Date:        c.row.Date,
			Amount:      c.row.Amount,
			Description: c.row.Description,
			ExternalID:  c.eid,
			Confidence:  c.row.Confidence,
		}
		_, isDup := existing[c.eid]
		_, isSeen := seen[c.eid]
		switch {
		case !c.row.Valid():
			rr.Status = ImportError
			rr.Error = c.row.Err
			res.ErrorCount++
		case isDup || isSeen:
			rr.Status = ImportDuplicate
			res.DuplicateCount++
		default:
			seen[c.eid] = struct{}{}
			rr.Status = ImportNew
			res.NewCount++
			// Low-confidence new rows are flagged for the user to confirm.
			// Deterministic CSV rows are 1.0 and never trip this.
			if c.row.Confidence < reviewConfidenceThreshold {
				rr.NeedsReview = true
				res.ReviewCount++
			}
			if commit {
				toInsert = append(toInsert, buildImportTxn(userID, account, source, c.row, c.eid, compiled))
			}
		}
		res.Rows = append(res.Rows, rr)
	}

	if commit && len(toInsert) > 0 {
		// One multi-row INSERT — atomic on its own; ON CONFLICT DO NOTHING on
		// the (account_id, external_id) index absorbs any row a concurrent
		// import inserted between our lookup and write.
		inserted, err := s.repo.ImportBatch(ctx, toInsert)
		if err != nil {
			return nil, fmt.Errorf("import batch: %w", err)
		}
		res.InsertedCount = int(inserted)
	}
	return res, nil
}

func buildImportTxn(userID int64, account *model.Account, source string, row ingestion.ParsedRow, eid string, compiled []categorization.CompiledRule) model.Transaction {
	eidCopy := eid
	t := model.Transaction{
		UserID:          userID,
		AccountID:       account.ID,
		AssetID:         account.PrimaryQuoteAssetID,
		Kind:            model.KindFlow,
		Amount:          row.Amount,
		TransactionDate: row.Date,
		Source:          source,
		ExternalID:      &eidCopy,
	}
	if desc := strings.TrimSpace(row.Description); desc != "" {
		t.Description = &desc
	}
	// No user-picked category on import — let rules categorize. Apply is a
	// no-op when compiled is empty, leaving the row uncategorized.
	categorization.Apply(&t, compiled)
	return t
}

// dedupKey is the content signature for a row, normalized so trivial
// whitespace differences don't defeat dedup.
func dedupKey(row ingestion.ParsedRow) string {
	return strings.Join([]string{
		row.Date.Format("2006-01-02"),
		row.Amount.String(),
		strings.ToLower(strings.TrimSpace(row.Description)),
	}, "|")
}

// importExternalID hashes the content key + occurrence index into a short,
// stable id, prefixed by source so the same statement re-imported as a
// different format doesn't masquerade as a duplicate. 8 bytes (16 hex chars)
// is ample collision resistance for a single account's statement history.
func importExternalID(source, base string, occurrence int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", base, occurrence)))
	return source + ":" + hex.EncodeToString(sum[:8])
}
