package ai

// This file holds the wire shape the document extractors parse provider
// responses into. The extractor *interface* is ingestion.Extractor (shared with
// the deterministic CSV extractor) — keeping it in ingestion avoids an import
// cycle, since service depends on ingestion but ai depends on service.
//
// A document extractor is a SEPARATE trust context from the chat Provider: it
// deliberately handles raw, PII-bearing statement bytes, so it is intentionally
// NOT bound by the chat provider's PII ban (ADR-0019 §2). The egress is gated by
// per-import consent and audited at the call site.

// extractedRow is one candidate transaction the model proposes. Every field is
// a raw string: the extractor re-parses Date/Amount through ingestion's
// deterministic validators (ingestion.NewRow) before any row is admitted, so a
// hallucinated value becomes an error row rather than a transaction
// (ADR-0019 §3).
type extractedRow struct {
	Date        string  `json:"date"`        // ideally YYYY-MM-DD; re-parsed downstream
	Amount      string  `json:"amount"`      // signed; negative = money out
	Description string  `json:"description"` // merchant / memo
	Confidence  float64 `json:"confidence"`  // 0–1, model's certainty in this row
}

// documentExtraction is a provider's structured output for one document.
// DocTotals are statement totals (closing balance, total debits, …) used to
// reconcile the row sum. Notes is free-form model commentary, for debugging.
type documentExtraction struct {
	Rows      []extractedRow `json:"rows"`
	DocTotals []string       `json:"doc_totals"`
	Notes     string         `json:"notes"`
}
