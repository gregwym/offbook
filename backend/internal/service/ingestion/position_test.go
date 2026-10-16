package ingestion

import (
	"testing"
	"time"
)

func TestNewPosition_Validation(t *testing.T) {
	def := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		symbol  string
		qty     string
		asOf    string
		wantErr bool
		wantQty string
	}{
		{"exact crypto precision", "btc", "0.05123456789012345", "", false, "0.05123456789012345"},
		{"thousands separator", "VTI", "1,250.5", "2026-05-30", false, "1250.5"},
		{"zero is admitted", "VTI", "0", "", false, "0"},
		{"negative rejected", "VTI", "-3", "", true, ""},
		{"garbage quantity", "VTI", "lots", "", true, ""},
		{"blank symbol", " ", "1", "", true, ""},
		{"bad as-of", "VTI", "1", "someday", true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPosition(1, tc.symbol, "", "weird-kind", tc.qty, "", "", tc.asOf, def, 0.9)
			if tc.wantErr {
				if p.Valid() {
					t.Fatalf("expected error, got valid %+v", p)
				}
				return
			}
			if !p.Valid() {
				t.Fatalf("unexpected error: %s", p.Err)
			}
			if p.Quantity.String() != tc.wantQty {
				t.Errorf("quantity = %s, want %s", p.Quantity, tc.wantQty)
			}
			if p.AssetKind != "equity" {
				t.Errorf("unknown kind should default to equity, got %q", p.AssetKind)
			}
		})
	}
}

func TestNewPosition_SymbolUppercasedAndDefaultAsOf(t *testing.T) {
	def := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	p := NewPosition(1, " vti ", "Vanguard", "fund", "2", "250.1", "oops", "", def, 1)
	if p.Symbol != "VTI" || !p.AsOf.Equal(def) || p.AssetKind != "fund" {
		t.Errorf("got %+v", p)
	}
	if p.Price == nil || p.Price.String() != "250.1" {
		t.Errorf("price = %v", p.Price)
	}
	if p.Value != nil {
		t.Errorf("unparseable value must be dropped, got %v", p.Value)
	}
	if bad := NewPosition(1, "VTI", "", "", "2", "", "", "", time.Time{}, 1); bad.Valid() {
		t.Error("missing as-of with no statement date must be rejected")
	}
}

func TestParseClosingBalance(t *testing.T) {
	if b, at, ok := ParseClosingBalance("15,234.56", "2026-05-31"); !ok || b.String() != "15234.56" || at.Day() != 31 {
		t.Errorf("got %v %v %v", b, at, ok)
	}
	for _, c := range [][2]string{{"", "2026-05-31"}, {"abc", "2026-05-31"}, {"10", ""}} {
		if _, _, ok := ParseClosingBalance(c[0], c[1]); ok {
			t.Errorf("%v should be absent", c)
		}
	}
}
