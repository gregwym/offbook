package ingestion

import (
	"errors"
	"slices"
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

func TestParse_DebitCreditColumns(t *testing.T) {
	cases := []struct {
		name    string
		csv     string
		mapping ColumnMapping
		want    []string // signed amounts, or "err:<substring>"
		debit   string
		credit  string
	}{
		{
			name:   "auto-detected unsigned pair",
			csv:    "Date,Description,Debit,Credit\n2026-05-15,Cafe,4.50,\n2026-05-16,Payroll,,2000.00\n",
			want:   []string{"-4.5", "2000"},
			debit:  "Debit",
			credit: "Credit",
		},
		{
			name:   "debits printed negative still reduce",
			csv:    "Date,Description,Withdrawals,Deposits\n2026-05-15,Cafe,-4.50,\n2026-05-16,Refund,,\"$1,000.01\"\n",
			want:   []string{"-4.5", "1000.01"},
			debit:  "Withdrawals",
			credit: "Deposits",
		},
		{
			name:   "both sides empty is an error row",
			csv:    "Date,Description,Debit,Credit\n2026-05-15,Nothing,,\n",
			want:   []string{"err:no debit or credit"},
			debit:  "Debit",
			credit: "Credit",
		},
		{
			name:   "unparseable debit",
			csv:    "Date,Description,Debit,Credit\n2026-05-15,Bad,abc,\n",
			want:   []string{"err:unparseable debit"},
			debit:  "Debit",
			credit: "Credit",
		},
		{
			name:    "explicit pair wins over an Amount column",
			csv:     "Date,Description,Amount,Out,In\n2026-05-15,Cafe,999,4.50,\n",
			mapping: ColumnMapping{Debit: "Out", Credit: "In"},
			want:    []string{"-4.5"},
			debit:   "Out",
			credit:  "In",
		},
		{
			name:    "explicit debit only",
			csv:     "Date,Description,Charge\n2026-05-15,Cafe,4.50\n",
			mapping: ColumnMapping{Debit: "Charge"},
			want:    []string{"-4.5"},
			debit:   "Charge",
		},
		{
			name: "single Amount column wins when both shapes exist",
			csv:  "Date,Description,Amount,Debit,Credit\n2026-05-15,Cafe,-4.50,9,\n",
			want: []string{"-4.5"},
		},
		{
			name:    "flip applies to split columns",
			csv:     "Date,Description,Debit,Credit\n2026-05-15,Cafe,4.50,\n",
			mapping: ColumnMapping{FlipSign: true},
			want:    []string{"4.5"},
			debit:   "Debit",
			credit:  "Credit",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Parse(strings.NewReader(tc.csv), tc.mapping)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if res.Mapping.Debit != tc.debit || res.Mapping.Credit != tc.credit {
				t.Errorf("mapping debit/credit = %q/%q, want %q/%q", res.Mapping.Debit, res.Mapping.Credit, tc.debit, tc.credit)
			}
			if len(res.Rows) != len(tc.want) {
				t.Fatalf("rows = %d, want %d", len(res.Rows), len(tc.want))
			}
			for i, w := range tc.want {
				r := res.Rows[i]
				if strings.HasPrefix(w, "err:") {
					if r.Valid() || !strings.Contains(r.Err, strings.TrimPrefix(w, "err:")) {
						t.Errorf("row %d err = %q, want %q", i, r.Err, w)
					}
					continue
				}
				if !r.Valid() || r.Amount.String() != w {
					t.Errorf("row %d = %s (err %q), want %s", i, r.Amount.String(), r.Err, w)
				}
			}
		})
	}
}

func TestParse_FlipSign(t *testing.T) {
	csv := "Date,Description,Amount\n2026-05-15,Restaurant,54.20\n2026-05-16,Payment thank you,-500.00\n2026-05-17,Bad,x\n"
	res, err := Parse(strings.NewReader(csv), ColumnMapping{FlipSign: true})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.Mapping.FlipSign {
		t.Errorf("mapping should echo flip_sign")
	}
	if got := res.Rows[0].Amount.String(); got != "-54.2" {
		t.Errorf("purchase = %s, want -54.2", got)
	}
	if got := res.Rows[1].Amount.String(); got != "500" {
		t.Errorf("payment = %s, want 500", got)
	}
	if res.Rows[2].Valid() || !res.Rows[2].Amount.IsZero() {
		t.Errorf("error row must stay an error with zero amount: %+v", res.Rows[2])
	}
}

func TestParse_DebitCreditUnmappable(t *testing.T) {
	cases := []struct {
		name    string
		csv     string
		mapping ColumnMapping
		missing string
	}{
		{"lone debit column is not auto-detected", "Date,Description,Debit\n2026-05-15,X,1\n", ColumnMapping{}, FieldAmount},
		{"requested debit header absent", "Date,Description,Debit,Credit\n2026-05-15,X,1,\n", ColumnMapping{Debit: "Out"}, FieldDebit},
		{"requested credit header absent", "Date,Description,Debit,Credit\n2026-05-15,X,1,\n", ColumnMapping{Debit: "Debit", Credit: "In"}, FieldCredit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.csv), tc.mapping)
			var ue *UnmappableError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want UnmappableError", err)
			}
			if !slices.Contains(ue.Missing, tc.missing) {
				t.Errorf("missing = %v, want to include %q", ue.Missing, tc.missing)
			}
		})
	}
}
