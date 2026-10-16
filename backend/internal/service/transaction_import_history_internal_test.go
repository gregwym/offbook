package service

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
)

func TestContentHash_StableAndContentSensitive(t *testing.T) {
	a, b := contentHash([]byte("x")), contentHash([]byte("x"))
	if a != b || len(a) != 64 {
		t.Fatalf("hash not stable/hex sha256: %q %q", a, b)
	}
	if contentHash([]byte("y")) == a {
		t.Fatal("different content produced same hash")
	}
}

func TestHistoryEntry_DifferenceOnMismatch(t *testing.T) {
	sum := decimal.RequireFromString("-95.50")
	no := false
	tests := []struct {
		name       string
		reconciled *bool
		totals     string
		wantDiff   string
	}{
		{"mismatch nearest total", &no, `["100.00","300"]`, "4.5"},
		{"sign convention ignored", &no, `["-99"]`, "3.5"},
		{"not applicable has no diff", nil, ``, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := model.IngestionJob{Reconciled: tc.reconciled, RowSum: &sum}
			if tc.totals != "" {
				j.DocTotals = []byte(tc.totals)
			}
			e := historyEntry(j)
			if e.Difference != tc.wantDiff {
				t.Errorf("difference = %q, want %q", e.Difference, tc.wantDiff)
			}
		})
	}
}
