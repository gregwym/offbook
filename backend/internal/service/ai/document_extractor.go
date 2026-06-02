package ai

import (
	"context"
	"errors"
)

// StatementDocument is a raw uploaded statement for AI extraction. It mirrors
// ingestion.Document but lives here so the ingestion package need not depend on
// the ai package — the import service owns the conversion between them.
type StatementDocument struct {
	Filename string
	MIME     string
	Data     []byte
}

// ExtractedRow is one candidate transaction the model proposes. Every field is
// a raw string: the import layer re-parses Date/Amount through ingestion's
// deterministic validators (ADR-0019 §3) before any row is admitted, so a
// hallucinated value becomes an error row rather than a transaction.
type ExtractedRow struct {
	Date        string  `json:"date"`        // ideally YYYY-MM-DD; re-parsed downstream
	Amount      string  `json:"amount"`      // signed; negative = money out
	Description string  `json:"description"` // merchant / memo
	Confidence  float64 `json:"confidence"`  // 0–1, model's certainty in this row
}

// DocumentExtraction is a provider's structured output for one document.
// DocTotals are statement totals (closing balance, total debits, …) used to
// reconcile the row sum. Notes is free-form model commentary, surfaced for
// debugging only.
type DocumentExtraction struct {
	Rows      []ExtractedRow `json:"rows"`
	DocTotals []string       `json:"doc_totals"`
	Notes     string         `json:"notes"`
}

// DocumentExtractor turns a raw statement into candidate rows. This is a
// SEPARATE trust context from the chat Provider: it deliberately handles raw,
// PII-bearing statement bytes, so it is intentionally NOT bound by the chat
// provider's PII ban (ADR-0019 §2). The egress is gated by per-import consent
// and audited at the call site, not here.
//
// Implementations: ClaudeExtractor (Ollama is an ADR-0019 fast-follow). New
// providers plug in by implementing this interface — the Go-native adapter
// model from ADR-0019 §6.
type DocumentExtractor interface {
	ExtractDocument(ctx context.Context, doc StatementDocument) (*DocumentExtraction, error)
	// Name is the stable provider id persisted in ingestion_jobs.provider.
	Name() string
}

// ErrUnsupportedMediaType is returned when a document's MIME type is one the
// extractor cannot send to its provider. The handler maps it to a 415/400.
var ErrUnsupportedMediaType = errors.New("ai: unsupported document media type")

// ErrEmptyExtraction signals the provider returned no parseable rows — a
// blank/garbled scan, or a non-statement document. Surfaced so the UI can ask
// the user to retry with a clearer file.
var ErrEmptyExtraction = errors.New("ai: extractor returned no rows")
