package plaid_test

import (
	"testing"

	"github.com/shopspring/decimal"

	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

func TestMapInvestmentTransaction_Buy_WritesPair(t *testing.T) {
	in := plaidsvc.PlaidInvestmentTransaction{
		PlaidTransactionID: "ptx-buy-1",
		PlaidAccountID:     "pacct-1",
		PlaidSecurityID:    ptr("sec-aapl"),
		Date:               "2026-05-10",
		Name:               "BUY AAPL",
		Quantity:           decimal.NewFromInt(10),   // 10 shares in
		Amount:             decimal.NewFromInt(1500), // $1500 left the account
		Price:              decimal.NewFromInt(150),
		Type:               "buy",
		Subtype:            "buy",
		IsoCurrencyCode:    "USD",
	}
	plan, err := plaidsvc.MapInvestmentTransaction(in, 1, 42 /*cashAsset*/, 7 /*securityAsset*/, 9)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if plan.Action != plaidsvc.ActionPair {
		t.Fatalf("action = %v, want ActionPair", plan.Action)
	}
	if !plan.Security.Amount.Equal(decimal.NewFromInt(10)) {
		t.Errorf("security amount = %s, want +10", plan.Security.Amount)
	}
	if !plan.Cash.Amount.Equal(decimal.NewFromInt(-1500)) {
		t.Errorf("cash amount = %s, want -1500 (Plaid +1500 → project -1500)", plan.Cash.Amount)
	}
	if plan.Security.AssetID != 9 || plan.Cash.AssetID != 7 {
		t.Errorf("asset ids: sec=%d cash=%d, want 9/7", plan.Security.AssetID, plan.Cash.AssetID)
	}
}

func TestMapInvestmentTransaction_Sell_WritesInversePair(t *testing.T) {
	in := plaidsvc.PlaidInvestmentTransaction{
		PlaidTransactionID: "ptx-sell-1",
		PlaidAccountID:     "pacct-1",
		PlaidSecurityID:    ptr("sec-aapl"),
		Date:               "2026-05-10",
		Name:               "SELL AAPL",
		// Plaid sign for sell: Quantity negative (shares out), Amount negative
		// (money in — Plaid uses positive=outflow for cash, so a sell is
		// negative amount).
		Quantity:        decimal.NewFromInt(-5),
		Amount:          decimal.NewFromInt(-800),
		Price:           decimal.NewFromInt(160),
		Type:            "sell",
		Subtype:         "sell",
		IsoCurrencyCode: "USD",
	}
	plan, err := plaidsvc.MapInvestmentTransaction(in, 1, 42, 7, 9)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if plan.Action != plaidsvc.ActionPair {
		t.Fatalf("action = %v, want ActionPair", plan.Action)
	}
	if !plan.Security.Amount.Equal(decimal.NewFromInt(-5)) {
		t.Errorf("security amount = %s, want -5 (sell outflow)", plan.Security.Amount)
	}
	if !plan.Cash.Amount.Equal(decimal.NewFromInt(800)) {
		t.Errorf("cash amount = %s, want +800 (sale proceeds in)", plan.Cash.Amount)
	}
}

func TestMapInvestmentTransaction_Dividend_SingleCashLeg(t *testing.T) {
	in := plaidsvc.PlaidInvestmentTransaction{
		PlaidTransactionID: "ptx-div-1",
		PlaidAccountID:     "pacct-1",
		Date:               "2026-05-10",
		Name:               "Dividend AAPL",
		// Cash dividend: Plaid uses Type=cash with subtype=dividend.
		// Quantity is 0; Amount is negative (money entering account).
		Quantity:        decimal.Zero,
		Amount:          decimal.NewFromInt(-25),
		Type:            "cash",
		Subtype:         "dividend",
		IsoCurrencyCode: "USD",
	}
	plan, err := plaidsvc.MapInvestmentTransaction(in, 1, 42, 7, 0)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if plan.Action != plaidsvc.ActionSingleCash {
		t.Fatalf("action = %v, want ActionSingleCash", plan.Action)
	}
	if plan.Security != nil {
		t.Error("Security leg should be nil for cash/dividend")
	}
	if !plan.Cash.Amount.Equal(decimal.NewFromInt(25)) {
		t.Errorf("cash amount = %s, want +25", plan.Cash.Amount)
	}
}

func TestMapInvestmentTransaction_Cancel_ReturnsCancelAction(t *testing.T) {
	in := plaidsvc.PlaidInvestmentTransaction{
		PlaidTransactionID:  "ptx-cancel",
		CancelTransactionID: ptr("ptx-buy-1"),
		PlaidAccountID:      "pacct-1",
		Date:                "2026-05-11",
		Type:                "cancel",
	}
	plan, err := plaidsvc.MapInvestmentTransaction(in, 1, 42, 7, 9)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if plan.Action != plaidsvc.ActionCancel {
		t.Fatalf("action = %v, want ActionCancel", plan.Action)
	}
	if plan.CancelPlaidTransactionID != "ptx-buy-1" {
		t.Errorf("cancel target = %q, want ptx-buy-1", plan.CancelPlaidTransactionID)
	}
}

func TestMapInvestmentTransaction_UnknownType_Ignored(t *testing.T) {
	in := plaidsvc.PlaidInvestmentTransaction{
		PlaidTransactionID: "ptx-weird",
		PlaidAccountID:     "pacct-1",
		Date:               "2026-05-10",
		Type:               "stocksplit", // future Plaid enum
	}
	plan, err := plaidsvc.MapInvestmentTransaction(in, 1, 42, 7, 9)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if plan.Action != plaidsvc.ActionIgnore {
		t.Errorf("action = %v, want ActionIgnore for unknown type", plan.Action)
	}
}

func TestMapInvestmentTransaction_BuyWithoutSecurityID_Errors(t *testing.T) {
	in := plaidsvc.PlaidInvestmentTransaction{
		PlaidTransactionID: "ptx-bad",
		PlaidAccountID:     "pacct-1",
		Date:               "2026-05-10",
		Type:               "buy",
		Quantity:           decimal.NewFromInt(1),
		Amount:             decimal.NewFromInt(100),
	}
	// securityAssetID = 0 — caller couldn't resolve.
	if _, err := plaidsvc.MapInvestmentTransaction(in, 1, 42, 7, 0); err == nil {
		t.Error("expected error when buy row has no resolvable security")
	}
}

func TestSecurityKind_Mapping(t *testing.T) {
	cases := []struct {
		plaidType string
		isCash    bool
		want      string
	}{
		{"equity", false, "equity"},
		{"mutual fund", false, "fund"},
		{"etf", false, "fund"},
		{"fixed income", false, "bond"},
		{"cryptocurrency", false, "crypto"},
		{"cash", false, "fiat"},
		{"derivative", true, "fiat"}, // is_cash_equivalent wins
		{"", false, "other"},
	}
	for _, tc := range cases {
		got := plaidsvc.SecurityKind(tc.plaidType, tc.isCash)
		if got != tc.want {
			t.Errorf("SecurityKind(%q, %v) = %q, want %q", tc.plaidType, tc.isCash, got, tc.want)
		}
	}
}
