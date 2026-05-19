// Package ingestion turns external statement files into Investment
// snapshots. The CSV parser here detects the brokerage format from the
// header row and produces a parsed result + per-row errors. The caller
// (InvestmentService) decides whether to persist them.
//
// Format detection rationale: Vanguard and Fidelity both ship "positions"
// exports with stable column names, but the surrounding rows differ —
// Fidelity prepends metadata, Vanguard sometimes has a trailing
// disclosure. We skip lines until we see a recognized header, then
// parse columns by header name (not index) so a re-ordering on the
// brokerage side doesn't silently corrupt data.
package ingestion

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ErrUnknownCSVFormat is returned when no supported header row is found
// in the file. Surfaced as a 400 to the user so they can correct the
// file rather than silently dropping rows.
var ErrUnknownCSVFormat = errors.New("unknown CSV format: expected Vanguard or Fidelity holdings export")

// ParsedHolding is one row from the CSV, normalized. Money + quantity
// arrive as decimal strings so service-layer NUMERIC arithmetic stays
// exact. AssetClass mirrors what the broker says (Fidelity provides
// "Type"; Vanguard doesn't include one — left empty).
type ParsedHolding struct {
	Ticker      string
	Name        string
	AssetClass  string
	Quantity    decimal.Decimal
	CostBasis   *decimal.Decimal
	MarketValue *decimal.Decimal
}

// RowError captures one CSV row that failed to parse. Line is the 1-based
// line number in the original file (not the data-row index) so users can
// jump to the offending row in a spreadsheet.
type RowError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// ParseResult is the full output of one CSV parse. SnapshotDate is the
// date we'll stamp on each created snapshot — derived from the CSV's
// own "Date downloaded" header when present, otherwise today's UTC date.
type ParseResult struct {
	Format       string // "vanguard" | "fidelity"
	SnapshotDate time.Time
	Holdings     []ParsedHolding
	Errors       []RowError
}

// ParseHoldingsCSV reads from r and returns a ParseResult. Returns
// ErrUnknownCSVFormat if no supported header is detected.
func ParseHoldingsCSV(r io.Reader) (*ParseResult, error) {
	// Use a Reader configured with FieldsPerRecord=-1 so trailing
	// disclosure rows with shorter fields don't kill the parse.
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	res := &ParseResult{
		SnapshotDate: time.Now().UTC().Truncate(24 * time.Hour),
		Holdings:     []ParsedHolding{},
		Errors:       []RowError{},
	}

	var headers []string
	var format string

	lineNum := 0
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Hard CSV error (e.g. unmatched quote) — fail the whole parse.
			return nil, fmt.Errorf("read csv: %w", err)
		}
		lineNum++

		// Try to lift the snapshot date from any pre-header row that looks
		// like "Date downloaded: 5/13/2026" or "As of 2026-05-13". Cheap to
		// check on every pre-header row.
		if format == "" {
			if d, ok := tryParseDate(row); ok {
				res.SnapshotDate = d
			}
			if f := detectFormat(row); f != "" {
				format = f
				headers = normalizeHeaders(row)
				continue
			}
			// Not a header — keep scanning.
			continue
		}

		// Past the header — every row should be a holding (or trailing
		// disclosure). Trailing rows usually have empty Symbol; we skip
		// silently rather than logging an error per disclosure line.
		holding, err := parseRow(format, headers, row)
		if err != nil {
			if errors.Is(err, errSkipRow) {
				continue
			}
			res.Errors = append(res.Errors, RowError{Line: lineNum, Message: err.Error()})
			continue
		}
		res.Holdings = append(res.Holdings, holding)
	}

	if format == "" {
		return nil, ErrUnknownCSVFormat
	}
	res.Format = format
	return res, nil
}

// errSkipRow signals a row is intentionally not an error — usually a
// trailing disclosure or all-empty line.
var errSkipRow = errors.New("skip row")

// detectFormat looks for the canonical first column of each broker's
// positions export. Header detection is case-insensitive and trims
// whitespace; brokers occasionally pad cells.
func detectFormat(row []string) string {
	hdr := normalizeHeaders(row)
	// Vanguard: starts with "Investment Name" and includes "Symbol",
	// "Shares", "Share Price", "Total Value".
	if has(hdr, "investment name") && has(hdr, "symbol") && has(hdr, "shares") {
		return "vanguard"
	}
	// Fidelity: "Symbol", "Description", "Quantity", "Last Price",
	// "Current Value", "Cost Basis Total", "Type".
	if has(hdr, "symbol") && has(hdr, "quantity") && has(hdr, "current value") {
		return "fidelity"
	}
	return ""
}

func normalizeHeaders(row []string) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = strings.ToLower(strings.TrimSpace(c))
	}
	return out
}

func has(headers []string, want string) bool {
	for _, h := range headers {
		if h == want {
			return true
		}
	}
	return false
}

func col(headers []string, row []string, name string) string {
	for i, h := range headers {
		if h == name {
			if i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
	}
	return ""
}

func parseRow(format string, headers []string, row []string) (ParsedHolding, error) {
	switch format {
	case "vanguard":
		return parseVanguardRow(headers, row)
	case "fidelity":
		return parseFidelityRow(headers, row)
	default:
		return ParsedHolding{}, fmt.Errorf("unknown format %q", format)
	}
}

func parseVanguardRow(headers, row []string) (ParsedHolding, error) {
	symbol := strings.ToUpper(col(headers, row, "symbol"))
	if symbol == "" {
		return ParsedHolding{}, errSkipRow
	}
	qtyStr := cleanNumeric(col(headers, row, "shares"))
	qty, err := decimal.NewFromString(qtyStr)
	if err != nil {
		return ParsedHolding{}, fmt.Errorf("shares %q: %w", qtyStr, err)
	}
	h := ParsedHolding{
		Ticker:   symbol,
		Name:     col(headers, row, "investment name"),
		Quantity: qty,
	}
	if mvStr := cleanNumeric(col(headers, row, "total value")); mvStr != "" {
		mv, err := decimal.NewFromString(mvStr)
		if err != nil {
			return ParsedHolding{}, fmt.Errorf("total value %q: %w", mvStr, err)
		}
		h.MarketValue = &mv
	}
	return h, nil
}

func parseFidelityRow(headers, row []string) (ParsedHolding, error) {
	symbol := strings.ToUpper(col(headers, row, "symbol"))
	if symbol == "" {
		return ParsedHolding{}, errSkipRow
	}
	// Fidelity sometimes uses prefix markers like "**" on pending tickers;
	// strip those so they don't trip the service-layer ticker check.
	symbol = strings.Trim(symbol, "* ")
	if symbol == "" {
		return ParsedHolding{}, errSkipRow
	}
	qtyStr := cleanNumeric(col(headers, row, "quantity"))
	qty, err := decimal.NewFromString(qtyStr)
	if err != nil {
		return ParsedHolding{}, fmt.Errorf("quantity %q: %w", qtyStr, err)
	}
	h := ParsedHolding{
		Ticker:     symbol,
		Name:       col(headers, row, "description"),
		AssetClass: col(headers, row, "type"),
		Quantity:   qty,
	}
	if mvStr := cleanNumeric(col(headers, row, "current value")); mvStr != "" {
		mv, err := decimal.NewFromString(mvStr)
		if err != nil {
			return ParsedHolding{}, fmt.Errorf("current value %q: %w", mvStr, err)
		}
		h.MarketValue = &mv
	}
	if cbStr := cleanNumeric(col(headers, row, "cost basis total")); cbStr != "" {
		cb, err := decimal.NewFromString(cbStr)
		if err != nil {
			return ParsedHolding{}, fmt.Errorf("cost basis total %q: %w", cbStr, err)
		}
		h.CostBasis = &cb
	}
	return h, nil
}

// cleanNumeric strips currency symbols, thousands separators, and
// surrounding whitespace. Returns "" for empty or "--" / "N/A" markers.
func cleanNumeric(s string) string {
	t := strings.TrimSpace(s)
	if t == "" || t == "--" || strings.EqualFold(t, "n/a") {
		return ""
	}
	// Parentheses around a number = negative (accounting style).
	negative := false
	if strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")") {
		negative = true
		t = t[1 : len(t)-1]
	}
	t = strings.ReplaceAll(t, "$", "")
	t = strings.ReplaceAll(t, ",", "")
	t = strings.TrimSpace(t)
	if negative && !strings.HasPrefix(t, "-") {
		t = "-" + t
	}
	return t
}

// tryParseDate scans the row for a "Date downloaded: ..." or "As of ..."
// pattern and returns the parsed UTC midnight. Returns ok=false if
// nothing matches — caller falls back to today.
func tryParseDate(row []string) (time.Time, bool) {
	for _, cell := range row {
		c := strings.TrimSpace(cell)
		for _, prefix := range []string{"Date downloaded:", "Date downloaded ", "As of:", "As of "} {
			if strings.HasPrefix(c, prefix) {
				val := strings.TrimSpace(strings.TrimPrefix(c, prefix))
				if t, ok := parseFlexibleDate(val); ok {
					return t, true
				}
			}
		}
	}
	return time.Time{}, false
}

func parseFlexibleDate(s string) (time.Time, bool) {
	layouts := []string{"2006-01-02", "1/2/2006", "01/02/2006", "1/2/06"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC().Truncate(24 * time.Hour), true
		}
	}
	return time.Time{}, false
}
