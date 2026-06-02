package ingestion

import (
	"bytes"
	"context"
	"errors"
	"strings"
)

// Errors an Extractor may return; handlers map them to HTTP codes. CSV's
// parser returns its own (ErrEmptyFile, *UnmappableError); these cover the AI
// document extractor.
var (
	// ErrUnsupportedMediaType: the document's MIME is one the extractor cannot
	// process (e.g. a zip). → 415.
	ErrUnsupportedMediaType = errors.New("ingestion: unsupported document media type")
	// ErrEmptyExtraction: the extractor returned no usable rows (blank scan,
	// non-statement document). → 422, "try a clearer file".
	ErrEmptyExtraction = errors.New("ingestion: extractor returned no rows")
)

// Document is a raw uploaded statement awaiting extraction. Data holds the
// full bytes (not a stream) because non-CSV extractors — the AI vision path in
// ADR-0019 phase 2 — need the whole document, and re-reading a multipart
// stream is awkward.
type Document struct {
	Filename string
	MIME     string
	Data     []byte
	// CSVMapping is an optional per-import column override, honored only by the
	// CSV extractor (columns are auto-detected when it is zero). Other
	// extractors ignore it.
	CSVMapping ColumnMapping
}

// Extraction is the neutral, source-tagged output every Extractor produces:
// the rows to import plus optional CSV echo metadata for the column-mapping
// fallback UI. Source is the value written to transactions.source.
type Extraction struct {
	Source string `json:"source"`
	// Rows serializes: the AI import path stages an Extraction as JSONB in
	// ingestion_jobs and rehydrates it on commit (ADR-0019 §7). Extraction is
	// an internal type — the API surface is ImportResult, not this.
	Rows    []ParsedRow   `json:"rows"`
	Mapping ColumnMapping `json:"mapping"`
	Headers []string      `json:"headers"`
	// DocTotals are statement totals an extractor detected (e.g. a closing
	// balance or "total debits") used to reconcile the row sum (ADR-0019 §3).
	// Empty for CSV, which carries no document-level total. decimal strings.
	DocTotals []string `json:"doc_totals,omitempty"`
}

// Extractor turns a Document into a neutral Extraction. The deterministic CSV
// extractor is the only implementation today; the AI extractor (ADR-0019
// phase 2) joins the registry for PDF/photo and unmappable-CSV fallback.
type Extractor interface {
	// Extract parses the document into rows. It returns the same domain errors
	// the underlying parser does (e.g. *UnmappableError) so the handler can map
	// them to 4xx codes.
	Extract(ctx context.Context, doc Document) (*Extraction, error)
	// Handles reports whether this extractor can process the given MIME type.
	Handles(mime string) bool
	// Name is a short stable identifier ("csv") used in logs and provenance.
	Name() string
}

// CSVExtractor adapts the deterministic CSV parser to the Extractor interface.
// Stateless.
type CSVExtractor struct{}

// Name implements Extractor.
func (CSVExtractor) Name() string { return "csv" }

// Handles matches the MIME types browsers and OSes attach to .csv uploads.
// text/plain is included because many clients send CSVs as plain text.
func (CSVExtractor) Handles(mime string) bool {
	switch normalizeMIME(mime) {
	case "text/csv", "application/csv", "text/plain", "application/vnd.ms-excel":
		return true
	default:
		return false
	}
}

// Extract parses the document bytes as CSV. doc.CSVMapping (when non-empty)
// overrides column auto-detection.
func (CSVExtractor) Extract(_ context.Context, doc Document) (*Extraction, error) {
	res, err := Parse(bytes.NewReader(doc.Data), doc.CSVMapping)
	if err != nil {
		return nil, err
	}
	return &Extraction{
		Source:  "csv",
		Rows:    res.Rows,
		Mapping: res.Mapping,
		Headers: res.Headers,
	}, nil
}

// Registry routes a Document to the Extractor that handles its MIME type. The
// fallback handles anything no registered extractor claims — in phase 1 that is
// the CSV extractor, preserving the pre-registry behavior of attempting a CSV
// parse on any upload.
type Registry struct {
	extractors []Extractor
	fallback   Extractor
}

// NewRegistry builds a registry with the given fallback (required) and any
// number of MIME-specific extractors tried ahead of it.
func NewRegistry(fallback Extractor, others ...Extractor) *Registry {
	return &Registry{extractors: others, fallback: fallback}
}

// NewDefaultRegistry returns the phase-1 registry: CSV only, as both the sole
// extractor and the fallback. ADR-0019 phase 2 will register the AI extractor
// for application/pdf and image/* ahead of this fallback.
func NewDefaultRegistry() *Registry {
	return NewRegistry(CSVExtractor{})
}

// For returns the extractor that handles mime, or the fallback when none does.
func (r *Registry) For(mime string) Extractor {
	for _, e := range r.extractors {
		if e.Handles(mime) {
			return e
		}
	}
	return r.fallback
}

// normalizeMIME lowercases and strips any "; charset=..." parameter so
// "text/csv; charset=utf-8" matches "text/csv".
func normalizeMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return mime
}
