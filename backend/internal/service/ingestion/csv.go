// Package ingestion parses external statement files (CSV today; PDF/photo
// later) into a neutral row shape the transaction service can import. It owns
// only parsing + field mapping — no DB access, no business rules. The service
// layer resolves the account asset, applies categorization rules, dedups, and
// persists. See issue #330.
package ingestion

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Field is a logical column the importer needs. CSV headers are mapped onto
// these. Amount follows Offbook's sign convention (positive = money in,
// negative = money out) — most bank exports already use negative for debits,
// so values pass through unflipped.
const (
	FieldDate        = "date"
	FieldAmount      = "amount"
	FieldDescription = "description"
	// FieldDebit / FieldCredit are the split-column alternative to FieldAmount
	// (#542): many exports put money out and money in in separate unsigned
	// columns. Signed amount = |credit| − |debit|.
	FieldDebit  = "debit"
	FieldCredit = "credit"
)

// ColumnMapping ties logical fields to source-CSV header names (matched
// case-insensitively, surrounding whitespace ignored). Empty values trigger
// auto-detection from a list of common aliases.
//
// The money comes from either Amount (one signed column) or the Debit +
// Credit pair. A requested Debit or Credit picks split mode; otherwise a
// single Amount column wins and the pair is the fallback. FlipSign negates
// every parsed amount, for exports that list purchases as positive (#542).
type ColumnMapping struct {
	Date        string `json:"date"`
	Amount      string `json:"amount"`
	Debit       string `json:"debit,omitempty"`
	Credit      string `json:"credit,omitempty"`
	Description string `json:"description"`
	FlipSign    bool   `json:"flip_sign,omitempty"`
}

// split reports whether the mapping reads money from Debit/Credit columns.
func (m ColumnMapping) split() bool { return m.Debit != "" || m.Credit != "" }

// headerAliases maps each logical field to header names commonly emitted by
// banks and brokerages. Lowercased; matched on trimmed, lowercased headers.
var headerAliases = map[string][]string{
	FieldDate:        {"date", "transaction date", "trans date", "posted date", "post date", "posting date"},
	FieldAmount:      {"amount", "transaction amount", "value", "amount (usd)"},
	FieldDescription: {"description", "name", "merchant", "merchant name", "payee", "memo", "details", "narrative"},
	FieldDebit:       {"debit", "debits", "debit amount", "withdrawal", "withdrawals", "withdrawal amount", "money out", "paid out"},
	FieldCredit:      {"credit", "credits", "credit amount", "deposit", "deposits", "deposit amount", "money in", "paid in"},
}

// dateLayouts are tried in order. Slash-separated dates are assumed US-style
// (MM/DD/YYYY) — an acknowledged limitation surfaced in the import preview.
var dateLayouts = []string{
	"2006-01-02",
	time.RFC3339,
	"2006/01/02",
	"01/02/2006",
	"1/2/2006",
	"01/02/06",
	"1/2/06",
	"01-02-2006",
	"02-Jan-2006",
	"Jan 2, 2006",
	"January 2, 2006",
}

// ParsedRow is one neutral, validated (or errored) statement line.
type ParsedRow struct {
	// Line is the 1-based source line number (header = line 1), for error
	// messages the user can act on.
	Line        int             `json:"line"`
	Date        time.Time       `json:"date"`
	Amount      decimal.Decimal `json:"amount"`
	Description string          `json:"description"`
	// Err is non-empty when this row could not be parsed; Date/Amount are then
	// zero and the row is excluded from import.
	Err string `json:"error,omitempty"`
	// Confidence is the extractor's certainty in this row, 0–1. Deterministic
	// parsing (CSV) sets 1.0 for valid rows — there is no ambiguity once the
	// columns are mapped. The AI extractor (ADR-0019 phase 2) reports a real
	// per-row confidence the import service gates "needs review" on.
	Confidence float64 `json:"confidence"`
}

// Valid reports whether the row parsed cleanly and can be imported.
func (r ParsedRow) Valid() bool { return r.Err == "" }

// ParseResult is the outcome of parsing a CSV: the resolved mapping (so the
// caller can echo detected columns back to the UI), the source headers, and
// every data row (valid or errored).
type ParseResult struct {
	Mapping ColumnMapping `json:"mapping"`
	Headers []string      `json:"headers"`
	Rows    []ParsedRow   `json:"rows"`
}

// ErrNoHeader / ErrUnmappable are domain errors the handler maps to 4xx.
var (
	ErrEmptyFile  = fmt.Errorf("file is empty")
	ErrNoDataRows = fmt.Errorf("file has a header but no data rows")
)

// UnmappableError reports which required fields could not be matched to a
// header during auto-detection, so the UI can ask the user to map them.
type UnmappableError struct {
	Missing []string
	Headers []string
}

func (e *UnmappableError) Error() string {
	return fmt.Sprintf("could not auto-detect columns for: %s (headers: %s)",
		strings.Join(e.Missing, ", "), strings.Join(e.Headers, ", "))
}

// Parse reads a CSV and returns one ParsedRow per data line. The supplied
// mapping wins; any empty field is auto-detected from header aliases. A row
// that fails date/amount parsing is returned with Err set rather than aborting
// the whole file — partial files are common and the caller reports per-row.
func Parse(r io.Reader, requested ColumnMapping) (*ParseResult, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate ragged rows; we validate per-row
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err == io.EOF {
		return nil, ErrEmptyFile
	}
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	header = trimAll(header)

	mapping, idx, err := resolveMapping(header, requested)
	if err != nil {
		return nil, err
	}

	res := &ParseResult{Mapping: mapping, Headers: header}
	line := 1 // header consumed
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			res.Rows = append(res.Rows, ParsedRow{Line: line, Err: fmt.Sprintf("malformed row: %v", err)})
			continue
		}
		if isBlank(rec) {
			continue // skip empty trailing lines silently
		}
		row := parseRecord(line, rec, idx)
		if mapping.FlipSign && row.Valid() {
			row.Amount = row.Amount.Neg()
		}
		res.Rows = append(res.Rows, row)
	}

	if len(res.Rows) == 0 {
		return nil, ErrNoDataRows
	}
	return res, nil
}

// fieldIndex holds the resolved column positions for each logical field.
// amount is -1 in split mode, where debit/credit hold the positions instead
// (either may be -1 when only one side was mapped).
type fieldIndex struct{ date, amount, debit, credit, description int }

func resolveMapping(header []string, requested ColumnMapping) (ColumnMapping, fieldIndex, error) {
	lower := make(map[string]int, len(header))
	for i, h := range header {
		lower[strings.ToLower(h)] = i
	}

	resolve := func(field, requestedName string) (string, int) {
		if requestedName != "" {
			if i, ok := lower[strings.ToLower(strings.TrimSpace(requestedName))]; ok {
				return header[i], i
			}
			return "", -1 // requested a header that doesn't exist
		}
		for _, alias := range headerAliases[field] {
			if i, ok := lower[alias]; ok {
				return header[i], i
			}
		}
		return "", -1
	}

	out := ColumnMapping{FlipSign: requested.FlipSign}
	idx := fieldIndex{amount: -1, debit: -1, credit: -1}
	var missing []string

	if name, i := resolve(FieldDate, requested.Date); i >= 0 {
		out.Date, idx.date = name, i
	} else {
		missing = append(missing, FieldDate)
	}
	// Money: an explicit Debit/Credit request means split mode. Otherwise try
	// one Amount column, then fall back to an auto-detected Debit + Credit pair.
	splitMode := requested.split()
	if !splitMode {
		if name, i := resolve(FieldAmount, requested.Amount); i >= 0 {
			out.Amount, idx.amount = name, i
		} else if requested.Amount == "" {
			_, di := resolve(FieldDebit, "")
			_, ci := resolve(FieldCredit, "")
			splitMode = di >= 0 && ci >= 0
		}
		if !splitMode && idx.amount < 0 {
			missing = append(missing, FieldAmount)
		}
	}
	if splitMode {
		// In split mode an explicitly requested side must exist; an omitted side
		// is auto-detected, and may be absent only if the other one resolved.
		dn, di := resolve(FieldDebit, requested.Debit)
		cn, ci := resolve(FieldCredit, requested.Credit)
		out.Debit, idx.debit = dn, di
		out.Credit, idx.credit = cn, ci
		if (requested.Debit != "" && di < 0) || (di < 0 && ci < 0) {
			missing = append(missing, FieldDebit)
		}
		if requested.Credit != "" && ci < 0 {
			missing = append(missing, FieldCredit)
		}
	}
	if name, i := resolve(FieldDescription, requested.Description); i >= 0 {
		out.Description, idx.description = name, i
	} else {
		missing = append(missing, FieldDescription)
	}

	if len(missing) > 0 {
		return out, idx, &UnmappableError{Missing: missing, Headers: header}
	}
	return out, idx, nil
}

func parseRecord(line int, rec []string, idx fieldIndex) ParsedRow {
	row := ParsedRow{Line: line}

	dateStr := field(rec, idx.date)
	d, err := parseDate(dateStr)
	if err != nil {
		row.Err = fmt.Sprintf("unparseable date %q", dateStr)
		return row
	}
	row.Date = d

	var amt decimal.Decimal
	if idx.amount >= 0 {
		amtStr := field(rec, idx.amount)
		amt, err = parseAmount(amtStr)
		if err != nil {
			row.Err = fmt.Sprintf("unparseable amount %q", amtStr)
			return row
		}
	} else {
		amt, row.Err = splitAmount(field(rec, idx.debit), field(rec, idx.credit))
		if row.Err != "" {
			return row
		}
	}
	if amt.IsZero() {
		row.Err = "amount is zero"
		return row
	}
	row.Amount = amt

	row.Description = field(rec, idx.description)
	// Deterministic parse: a cleanly parsed row is fully trusted.
	row.Confidence = 1.0
	return row
}

// NewRow builds a ParsedRow from raw string fields, running them through the
// exact same date/amount validators the CSV parser uses. This is the
// re-validation seam from ADR-0019 §3: the AI extractor produces raw strings,
// and feeding them here guarantees an LLM-proposed row is admitted only if its
// date and amount parse deterministically — a hallucinated "amount" of "lots"
// becomes an error row, never a transaction. confidence is the extractor's
// per-row certainty (0–1); a failed parse sets Err and the row is excluded.
func NewRow(line int, dateStr, amountStr, description string, confidence float64) ParsedRow {
	row := ParsedRow{Line: line, Description: strings.TrimSpace(description), Confidence: confidence}
	d, err := parseDate(dateStr)
	if err != nil {
		row.Err = fmt.Sprintf("unparseable date %q", dateStr)
		return row
	}
	row.Date = d
	amt, err := parseAmount(amountStr)
	if err != nil {
		row.Err = fmt.Sprintf("unparseable amount %q", amountStr)
		return row
	}
	if amt.IsZero() {
		row.Err = "amount is zero"
		return row
	}
	row.Amount = amt
	return row
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("no layout matched")
}

// splitAmount combines a Debit/Credit pair into one signed amount: money in
// minus money out. Signs inside the columns are ignored (some banks print
// debits as negatives, most as unsigned), so a debit always reduces the
// balance. An empty side counts as zero; both empty is an error.
func splitAmount(debitStr, creditStr string) (decimal.Decimal, string) {
	if debitStr == "" && creditStr == "" {
		return decimal.Zero, "no debit or credit amount"
	}
	out, in := decimal.Zero, decimal.Zero
	if debitStr != "" {
		d, err := parseAmount(debitStr)
		if err != nil {
			return decimal.Zero, fmt.Sprintf("unparseable debit %q", debitStr)
		}
		out = d.Abs()
	}
	if creditStr != "" {
		c, err := parseAmount(creditStr)
		if err != nil {
			return decimal.Zero, fmt.Sprintf("unparseable credit %q", creditStr)
		}
		in = c.Abs()
	}
	return in.Sub(out), ""
}

// parseAmount strips currency symbols, thousands separators, and surrounding
// whitespace, and treats (123.45) accounting notation as -123.45.
func parseAmount(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero, fmt.Errorf("empty")
	}
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}
	// Drop currency symbols, thousands commas, spaces, and a leading +.
	replacer := strings.NewReplacer("$", "", "€", "", "£", "", ",", "", " ", "", "+", "")
	s = replacer.Replace(s)
	if s == "" || s == "-" {
		return decimal.Zero, fmt.Errorf("no digits")
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, err
	}
	if neg {
		d = d.Neg()
	}
	return d, nil
}

func trimAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

func isBlank(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

func field(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}
