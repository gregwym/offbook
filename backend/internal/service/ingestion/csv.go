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
)

// ColumnMapping ties logical fields to source-CSV header names (matched
// case-insensitively, surrounding whitespace ignored). Empty values trigger
// auto-detection from a list of common aliases.
type ColumnMapping struct {
	Date        string `json:"date"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
}

func (m ColumnMapping) complete() bool {
	return m.Date != "" && m.Amount != "" && m.Description != ""
}

// headerAliases maps each logical field to header names commonly emitted by
// banks and brokerages. Lowercased; matched on trimmed, lowercased headers.
var headerAliases = map[string][]string{
	FieldDate:        {"date", "transaction date", "trans date", "posted date", "post date", "posting date"},
	FieldAmount:      {"amount", "transaction amount", "value", "amount (usd)"},
	FieldDescription: {"description", "name", "merchant", "merchant name", "payee", "memo", "details", "narrative"},
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
		res.Rows = append(res.Rows, parseRecord(line, rec, idx))
	}

	if len(res.Rows) == 0 {
		return nil, ErrNoDataRows
	}
	return res, nil
}

// fieldIndex holds the resolved column positions for each logical field.
type fieldIndex struct{ date, amount, description int }

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

	var out ColumnMapping
	var idx fieldIndex
	var missing []string

	if name, i := resolve(FieldDate, requested.Date); i >= 0 {
		out.Date, idx.date = name, i
	} else {
		missing = append(missing, FieldDate)
	}
	if name, i := resolve(FieldAmount, requested.Amount); i >= 0 {
		out.Amount, idx.amount = name, i
	} else {
		missing = append(missing, FieldAmount)
	}
	if name, i := resolve(FieldDescription, requested.Description); i >= 0 {
		out.Description, idx.description = name, i
	} else {
		missing = append(missing, FieldDescription)
	}

	if len(missing) > 0 {
		return out, idx, &UnmappableError{Missing: missing, Headers: header}
	}
	_ = out.complete()
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

	amtStr := field(rec, idx.amount)
	amt, err := parseAmount(amtStr)
	if err != nil {
		row.Err = fmt.Sprintf("unparseable amount %q", amtStr)
		return row
	}
	if amt.IsZero() {
		row.Err = "amount is zero"
		return row
	}
	row.Amount = amt

	row.Description = field(rec, idx.description)
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
