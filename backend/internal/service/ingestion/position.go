package ingestion

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ParsedPosition is one holding line an extractor read from a brokerage
// statement (#375). Like ParsedRow, every field is re-parsed deterministically
// from the model's raw strings: AI proposes, deterministic code admits. A
// failed parse sets Err and the position is excluded from reconciliation.
type ParsedPosition struct {
	Line      int    `json:"line"`
	Symbol    string `json:"symbol"`
	Name      string `json:"name,omitempty"`
	AssetKind string `json:"asset_kind"` // equity | fund | crypto | bond | other
	// Quantity is the units held per the statement (exact decimal).
	Quantity decimal.Decimal `json:"quantity"`
	// Price and Value are informational (preview only); they never feed the
	// ledger — quantities are facts, valuations derive from positions × prices.
	Price      *decimal.Decimal `json:"price,omitempty"`
	Value      *decimal.Decimal `json:"value,omitempty"`
	AsOf       time.Time        `json:"as_of"`
	Confidence float64          `json:"confidence"`
	Err        string           `json:"err,omitempty"`
}

// Valid reports whether the position parsed cleanly.
func (p ParsedPosition) Valid() bool { return p.Err == "" }

var validPositionKinds = map[string]bool{
	"equity": true, "fund": true, "crypto": true, "bond": true, "other": true,
}

// NewPosition validates an AI-proposed holding. asOfDefault (the statement
// date) is used when the line carries none. Zero quantities are admitted (a
// fully sold position the statement still lists reconciles the fold to 0);
// negative quantities are rejected (shorts are out of scope).
func NewPosition(line int, symbol, name, kind, qty, price, value, asOf string, asOfDefault time.Time, confidence float64) ParsedPosition {
	p := ParsedPosition{
		Line:       line,
		Symbol:     strings.ToUpper(strings.TrimSpace(symbol)),
		Name:       strings.TrimSpace(name),
		AssetKind:  strings.ToLower(strings.TrimSpace(kind)),
		Confidence: confidence,
	}
	if p.Symbol == "" || len(p.Symbol) > 32 || strings.ContainsAny(p.Symbol, " \t\n|") {
		p.Err = fmt.Sprintf("invalid symbol %q", symbol)
		return p
	}
	if !validPositionKinds[p.AssetKind] {
		p.AssetKind = "equity"
	}
	q, err := parseAmount(qty)
	if err != nil {
		p.Err = fmt.Sprintf("unparseable quantity %q", qty)
		return p
	}
	if q.IsNegative() {
		p.Err = "negative quantity"
		return p
	}
	p.Quantity = q
	if strings.TrimSpace(price) != "" {
		if d, err := parseAmount(price); err == nil && !d.IsNegative() {
			p.Price = &d
		}
	}
	if strings.TrimSpace(value) != "" {
		if d, err := parseAmount(value); err == nil {
			p.Value = &d
		}
	}
	p.AsOf = asOfDefault
	if strings.TrimSpace(asOf) != "" {
		d, err := parseDate(asOf)
		if err != nil {
			p.Err = fmt.Sprintf("unparseable as-of date %q", asOf)
			return p
		}
		p.AsOf = d
	}
	if p.AsOf.IsZero() {
		p.Err = "missing as-of date"
	}
	return p
}

// ParseClosingBalance validates the statement closing balance and its date.
// Returns ok=false when the balance is blank/unparseable (treated as absent,
// never fatal: the transaction rows stay importable).
func ParseClosingBalance(balance, asOf string) (bal decimal.Decimal, at time.Time, ok bool) {
	if strings.TrimSpace(balance) == "" {
		return decimal.Zero, time.Time{}, false
	}
	b, err := parseAmount(balance)
	if err != nil {
		return decimal.Zero, time.Time{}, false
	}
	t, err := parseDate(asOf)
	if err != nil {
		return decimal.Zero, time.Time{}, false
	}
	return b, t, true
}

// ParseStatementDate parses an extractor-proposed statement date; ok=false on
// blank/unparseable input.
func ParseStatementDate(s string) (time.Time, bool) {
	t, err := parseDate(s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
