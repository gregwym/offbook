package ingestion

import (
	"errors"
	"strings"
	"testing"
)

func TestParse_AutoDetectMapping(t *testing.T) {
	csv := "Date,Description,Amount\n2026-05-15,Coffee,-4.50\n2026-05-16,Paycheck,2000.00\n"
	res, err := Parse(strings.NewReader(csv), ColumnMapping{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Mapping.Date != "Date" || res.Mapping.Amount != "Amount" || res.Mapping.Description != "Description" {
		t.Fatalf("auto-detected mapping = %+v, want Date/Amount/Description", res.Mapping)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if !res.Rows[0].Valid() || res.Rows[0].Description != "Coffee" || res.Rows[0].Amount.String() != "-4.5" {
		t.Errorf("row 0 = %+v", res.Rows[0])
	}
	if res.Rows[0].Line != 2 {
		t.Errorf("row 0 line = %d, want 2 (header is line 1)", res.Rows[0].Line)
	}
}

func TestParse_AliasHeaders(t *testing.T) {
	// "Posted Date", "Payee", "Transaction Amount" are all aliases.
	csv := "Posted Date,Payee,Transaction Amount\n01/15/2026,Store,-9.99\n"
	res, err := Parse(strings.NewReader(csv), ColumnMapping{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Mapping.Date != "Posted Date" || res.Mapping.Description != "Payee" || res.Mapping.Amount != "Transaction Amount" {
		t.Fatalf("mapping = %+v", res.Mapping)
	}
	if res.Rows[0].Date.Format("2006-01-02") != "2026-01-15" {
		t.Errorf("date = %s, want 2026-01-15 (US MM/DD/YYYY)", res.Rows[0].Date.Format("2006-01-02"))
	}
}

func TestParse_ExplicitMappingOverridesAutoDetect(t *testing.T) {
	// Two columns could each look like a description; pin them explicitly.
	csv := "when,memo,note,value\n2026-05-15,real-desc,ignored,12.00\n"
	res, err := Parse(strings.NewReader(csv), ColumnMapping{Date: "when", Amount: "value", Description: "memo"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Rows[0].Description != "real-desc" {
		t.Errorf("description = %q, want real-desc", res.Rows[0].Description)
	}
	if res.Rows[0].Amount.String() != "12" {
		t.Errorf("amount = %s, want 12", res.Rows[0].Amount.String())
	}
}

func TestParse_AmountFormats(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"-12.34", "-12.34"},
		{"$1,234.56", "1234.56"},
		{"(45.00)", "-45"},
		{"+100", "100"},
		{"  -7.5 ", "-7.5"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			// Quote the amount so a thousands separator stays in one CSV field,
			// as real bank exports do.
			csv := "Date,Description,Amount\n2026-05-15,X,\"" + tc.raw + "\"\n"
			res, err := Parse(strings.NewReader(csv), ColumnMapping{})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !res.Rows[0].Valid() {
				t.Fatalf("row errored: %s", res.Rows[0].Err)
			}
			if got := res.Rows[0].Amount.String(); got != tc.want {
				t.Errorf("amount %q parsed to %s, want %s", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParse_ZeroAmountIsError(t *testing.T) {
	csv := "Date,Description,Amount\n2026-05-15,X,0.00\n"
	res, err := Parse(strings.NewReader(csv), ColumnMapping{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Rows[0].Valid() {
		t.Errorf("zero amount should error, got valid row")
	}
}

func TestParse_BadDateIsRowError_OthersSurvive(t *testing.T) {
	csv := "Date,Description,Amount\nnot-a-date,Bad,-1.00\n2026-05-16,Good,-2.00\n"
	res, err := Parse(strings.NewReader(csv), ColumnMapping{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if res.Rows[0].Valid() {
		t.Errorf("row 0 should be an error row")
	}
	if !res.Rows[1].Valid() {
		t.Errorf("row 1 should be valid, got: %s", res.Rows[1].Err)
	}
}

func TestParse_BlankLinesSkipped(t *testing.T) {
	csv := "Date,Description,Amount\n2026-05-15,X,-1.00\n\n   \n2026-05-16,Y,-2.00\n"
	res, err := Parse(strings.NewReader(csv), ColumnMapping{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (blank lines skipped)", len(res.Rows))
	}
}

func TestParse_UnmappableColumns(t *testing.T) {
	csv := "foo,bar,baz\n1,2,3\n"
	_, err := Parse(strings.NewReader(csv), ColumnMapping{})
	var unmappable *UnmappableError
	if !errors.As(err, &unmappable) {
		t.Fatalf("err = %v, want *UnmappableError", err)
	}
	if len(unmappable.Missing) != 3 {
		t.Errorf("missing = %v, want all three fields", unmappable.Missing)
	}
}

func TestParse_EmptyAndHeaderOnly(t *testing.T) {
	if _, err := Parse(strings.NewReader(""), ColumnMapping{}); !errors.Is(err, ErrEmptyFile) {
		t.Errorf("empty file err = %v, want ErrEmptyFile", err)
	}
	if _, err := Parse(strings.NewReader("Date,Description,Amount\n"), ColumnMapping{}); !errors.Is(err, ErrNoDataRows) {
		t.Errorf("header-only err = %v, want ErrNoDataRows", err)
	}
}
