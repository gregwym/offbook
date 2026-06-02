package ingestion

import (
	"context"
	"testing"
)

func TestCSVExtractor_Handles(t *testing.T) {
	cases := []struct {
		mime string
		want bool
	}{
		{"text/csv", true},
		{"text/csv; charset=utf-8", true},
		{"TEXT/CSV", true},
		{"application/csv", true},
		{"application/vnd.ms-excel", true},
		{"text/plain", true},
		{"application/pdf", false},
		{"image/png", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.mime, func(t *testing.T) {
			if got := (CSVExtractor{}).Handles(c.mime); got != c.want {
				t.Errorf("Handles(%q) = %v, want %v", c.mime, got, c.want)
			}
		})
	}
}

func TestCSVExtractor_Extract(t *testing.T) {
	csv := "Date,Description,Amount\n2026-05-15,Coffee,-4.50\n"
	doc := Document{MIME: "text/csv", Data: []byte(csv)}
	ext, err := CSVExtractor{}.Extract(context.Background(), doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if ext.Source != "csv" {
		t.Errorf("source = %q, want csv", ext.Source)
	}
	if len(ext.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ext.Rows))
	}
	if r := ext.Rows[0]; !r.Valid() || r.Confidence != 1.0 {
		t.Errorf("row valid=%v confidence=%v, want true/1.0", r.Valid(), r.Confidence)
	}
	// CSV echo metadata is populated for the manual-mapping fallback UI.
	if len(ext.Headers) != 3 || ext.Mapping.Amount == "" {
		t.Errorf("expected CSV echo metadata, got headers=%v mapping=%+v", ext.Headers, ext.Mapping)
	}
}

func TestCSVExtractor_ExtractPropagatesUnmappable(t *testing.T) {
	// No mappable columns → the extractor surfaces *UnmappableError unchanged so
	// the handler can return IMPORT_UNMAPPABLE.
	doc := Document{MIME: "text/csv", Data: []byte("foo,bar\n1,2\n")}
	_, err := CSVExtractor{}.Extract(context.Background(), doc)
	var unmappable *UnmappableError
	if !asUnmappable(err, &unmappable) {
		t.Fatalf("err = %v, want *UnmappableError", err)
	}
}

func TestRegistry_For(t *testing.T) {
	reg := NewDefaultRegistry()
	// Phase 1: CSV is the fallback, so any MIME routes to it.
	for _, mime := range []string{"text/csv", "application/pdf", "image/png", ""} {
		if got := reg.For(mime).Name(); got != "csv" {
			t.Errorf("For(%q) = %q, want csv (phase-1 fallback)", mime, got)
		}
	}
}

// asUnmappable is a tiny errors.As shim kept local so the test reads cleanly.
func asUnmappable(err error, target **UnmappableError) bool {
	if err == nil {
		return false
	}
	u, ok := err.(*UnmappableError)
	if ok {
		*target = u
	}
	return ok
}
