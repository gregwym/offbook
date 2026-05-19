package ingestion_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// Vanguard sample shape: a brief disclaimer/date row, blank line, then
// the header. This mirrors what a real Positions export looks like.
const vanguardCSV = `Date downloaded: 5/13/2026
Account Summary

Investment Name,Symbol,Shares,Share Price,Total Value
Vanguard Total Stock Market Index Fund,VTSAX,142.0000,$112.45,"$15,967.90"
Vanguard Total Bond Market Index Fund,VBTLX,89.5000,$10.21,$913.80

Important disclosures: returns are illustrative.
`

// Fidelity sample shape: header on the first line, holdings, trailing
// blank line + disclaimer. Includes a row with "**" prefix on the symbol
// (Fidelity uses this for pending settlements) — should be stripped.
const fidelityCSV = `Symbol,Description,Quantity,Last Price,Current Value,Cost Basis Total,Average Cost Basis,Type
AAPL,APPLE INC,28.0000,$184.12,"$5,155.36","$3,000.00",$107.14,Cash
**TSLA,TESLA INC,5.0000,$612.40,"$3,062.00","$2,500.00",$500.00,Cash
"BTCUSD","Bitcoin","0.05123456789012345",$104210.00,"$5,341.22",,,Crypto

The data above is for informational purposes only.
`

func TestParseHoldingsCSV_Vanguard(t *testing.T) {
	res, err := ingestion.ParseHoldingsCSV(strings.NewReader(vanguardCSV))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Format != "vanguard" {
		t.Errorf("format = %q, want vanguard", res.Format)
	}
	if len(res.Holdings) != 2 {
		t.Fatalf("len(holdings) = %d, want 2", len(res.Holdings))
	}
	if res.SnapshotDate.Format("2006-01-02") != "2026-05-13" {
		t.Errorf("snapshot date = %s, want 2026-05-13", res.SnapshotDate.Format("2006-01-02"))
	}
	vtsax := res.Holdings[0]
	if vtsax.Ticker != "VTSAX" {
		t.Errorf("ticker = %q, want VTSAX", vtsax.Ticker)
	}
	if !vtsax.Quantity.Equal(decimal.NewFromFloat(142.0)) {
		t.Errorf("quantity = %s, want 142", vtsax.Quantity)
	}
	if vtsax.MarketValue == nil || !vtsax.MarketValue.Equal(decimal.NewFromFloat(15967.90)) {
		t.Errorf("market value = %v, want 15967.90", vtsax.MarketValue)
	}
	if vtsax.Name == "" {
		t.Errorf("name empty; expected Vanguard Total Stock Market Index Fund")
	}
	if vtsax.CostBasis != nil {
		t.Errorf("Vanguard layout has no cost basis col; got %v", vtsax.CostBasis)
	}
}

func TestParseHoldingsCSV_Fidelity(t *testing.T) {
	res, err := ingestion.ParseHoldingsCSV(strings.NewReader(fidelityCSV))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Format != "fidelity" {
		t.Errorf("format = %q, want fidelity", res.Format)
	}
	if len(res.Holdings) != 3 {
		t.Fatalf("len(holdings) = %d, want 3 (AAPL, TSLA, BTCUSD)", len(res.Holdings))
	}
	// AAPL
	if h := res.Holdings[0]; h.Ticker != "AAPL" || h.AssetClass != "Cash" {
		t.Errorf("AAPL row = %+v", h)
	}
	if h := res.Holdings[0]; h.CostBasis == nil || !h.CostBasis.Equal(decimal.NewFromInt(3000)) {
		t.Errorf("AAPL cost basis = %v, want 3000", h.CostBasis)
	}
	// TSLA: "**TSLA" → "TSLA" after stripping the pending-settlement marker.
	if h := res.Holdings[1]; h.Ticker != "TSLA" {
		t.Errorf("TSLA row ticker = %q, want TSLA (after stripping **)", h.Ticker)
	}
	// Crypto: 18-decimal quantity round-trip is the literal M6 acceptance criterion.
	if h := res.Holdings[2]; h.Ticker != "BTCUSD" {
		t.Errorf("BTC row ticker = %q", h.Ticker)
	}
	wantBTCQty, _ := decimal.NewFromString("0.05123456789012345")
	if !res.Holdings[2].Quantity.Equal(wantBTCQty) {
		t.Errorf("BTC quantity = %s, want 0.05123456789012345 (precision)",
			res.Holdings[2].Quantity.String())
	}
}

func TestParseHoldingsCSV_UnknownFormat(t *testing.T) {
	// Header doesn't match either broker's canonical fields.
	bad := `Account,Asset,Units,Worth
ABC,Foo,10,100
`
	_, err := ingestion.ParseHoldingsCSV(strings.NewReader(bad))
	if !errors.Is(err, ingestion.ErrUnknownCSVFormat) {
		t.Errorf("err = %v, want ErrUnknownCSVFormat", err)
	}
}

func TestParseHoldingsCSV_PerRowError(t *testing.T) {
	// One row has a junk quantity. The good row should still parse.
	mixed := `Symbol,Description,Quantity,Last Price,Current Value,Cost Basis Total,Average Cost Basis,Type
AAPL,APPLE INC,10,$184,$1840,$1500,$150,Cash
BAD,FAILS HERE,notanumber,$10,$10,$10,$10,Cash
`
	res, err := ingestion.ParseHoldingsCSV(strings.NewReader(mixed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Holdings) != 1 || res.Holdings[0].Ticker != "AAPL" {
		t.Errorf("expected only AAPL to land, got %+v", res.Holdings)
	}
	if len(res.Errors) != 1 {
		t.Errorf("expected 1 row error, got %d (%+v)", len(res.Errors), res.Errors)
	}
}

func TestParseHoldingsCSV_AccountingNegatives(t *testing.T) {
	// Fidelity reports an unrealized loss as a parenthesized number on
	// some exports. We don't ingest G/L itself, but cost-basis-total
	// uses the same numeric cleaner, so the round-trip must work.
	csv := `Symbol,Description,Quantity,Last Price,Current Value,Cost Basis Total,Average Cost Basis,Type
XYZ,Test,1,$10,"$5","($10.00)",$10,Cash
`
	res, err := ingestion.ParseHoldingsCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Holdings) != 1 {
		t.Fatalf("len = %d, want 1", len(res.Holdings))
	}
	if res.Holdings[0].CostBasis == nil || !res.Holdings[0].CostBasis.Equal(decimal.NewFromFloat(-10.0)) {
		t.Errorf("cost basis = %v, want -10 (accounting parens)", res.Holdings[0].CostBasis)
	}
}
