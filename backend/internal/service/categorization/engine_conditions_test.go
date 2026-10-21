package categorization_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

func dec(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

func i64(v int64) *int64 { return &v }

// TestMatch_AccountAndAmountConditions covers #550: account and |amount|
// conditions are ANDed with the text matcher, bounds are inclusive and exact.
func TestMatch_AccountAndAmountConditions(t *testing.T) {
	cases := []struct {
		name      string
		rule      model.CategorizationRule
		accountID int64
		amount    string
		wantMatch bool
	}{
		{"no conditions", model.CategorizationRule{}, 7, "-15.49", true},
		{"account matches", model.CategorizationRule{AccountID: i64(7)}, 7, "-15.49", true},
		{"other account", model.CategorizationRule{AccountID: i64(8)}, 7, "-15.49", false},
		{"inside band", model.CategorizationRule{AmountMin: dec("10"), AmountMax: dec("20")}, 7, "-15.49", true},
		{"refund inside band (magnitude)", model.CategorizationRule{AmountMin: dec("10"), AmountMax: dec("20")}, 7, "15.49", true},
		{"min bound inclusive", model.CategorizationRule{AmountMin: dec("15.49")}, 7, "-15.49", true},
		{"just below min", model.CategorizationRule{AmountMin: dec("15.490000000000000001")}, 7, "-15.49", false},
		{"max bound inclusive", model.CategorizationRule{AmountMax: dec("15.49")}, 7, "-15.49", true},
		{"just above max", model.CategorizationRule{AmountMax: dec("15.489999999999999999")}, 7, "-15.49", false},
		{"open-ended min only", model.CategorizationRule{AmountMin: dec("500")}, 7, "-1200.00", true},
		{"account and amount both required", model.CategorizationRule{AccountID: i64(7), AmountMax: dec("5")}, 7, "-15.49", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.rule
			r.ID, r.CategoryID, r.MatchType, r.Pattern, r.IsActive = 1, 9, "contains", "NETFLIX", true
			rules := categorization.Compile([]model.CategorizationRule{r})
			tx := &model.Transaction{AccountID: tc.accountID, Amount: decimal.RequireFromString(tc.amount), Description: ptr("NETFLIX.COM")}
			_, ok := categorization.Match(rules, tx)
			if ok != tc.wantMatch {
				t.Errorf("Match = %v, want %v", ok, tc.wantMatch)
			}
		})
	}
}

// TestMatch_AmountBandsSplitOneMerchant: two rules on the same merchant
// route rows by amount band; a row outside both falls through.
func TestMatch_AmountBandsSplitOneMerchant(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "AMZN", AmountMax: dec("20"), IsActive: true},
		{ID: 2, CategoryID: 20, MatchType: "contains", Pattern: "AMZN", AmountMin: dec("500"), IsActive: true},
	})
	cases := []struct {
		amount  string
		wantCat int64
		wantOK  bool
	}{
		{"-12.99", 10, true},
		{"-20", 10, true},
		{"-649.00", 20, true},
		{"-89.50", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.amount, func(t *testing.T) {
			tx := &model.Transaction{AccountID: 1, Amount: decimal.RequireFromString(tc.amount), Description: ptr("AMZN Mktp US")}
			d, ok := categorization.Match(rules, tx)
			if ok != tc.wantOK || d.CategoryID != tc.wantCat {
				t.Errorf("Match = (%d, %v), want (%d, %v)", d.CategoryID, ok, tc.wantCat, tc.wantOK)
			}
		})
	}
}

// TestCategorize_SkipsConditionedRules: Categorize has no account or amount,
// so a conditioned rule must not fire there (it would over-match).
func TestCategorize_SkipsConditionedRules(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "VENMO", AccountID: i64(3), IsActive: true},
		{ID: 2, CategoryID: 20, MatchType: "contains", Pattern: "VENMO", AmountMin: dec("1"), IsActive: true},
	})
	if _, ok := categorization.Categorize(rules, 0, nil, ptr("VENMO PAYMENT"), nil); ok {
		t.Error("Categorize matched a conditioned rule without account/amount")
	}
}

// TestApply_UsesConditions: the import path (Apply) honors the conditions.
func TestApply_UsesConditions(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "VENMO", AccountID: i64(3), IsActive: true},
	})
	joint := &model.Transaction{AccountID: 3, Amount: decimal.RequireFromString("-1800"), Description: ptr("VENMO PAYMENT")}
	if _, ok := categorization.Apply(joint, rules); !ok || joint.CategoryID == nil || *joint.CategoryID != 10 {
		t.Errorf("joint-account row not categorized: %+v", joint.CategoryID)
	}
	other := &model.Transaction{AccountID: 4, Amount: decimal.RequireFromString("-1800"), Description: ptr("VENMO PAYMENT")}
	if _, ok := categorization.Apply(other, rules); ok || other.CategoryID != nil {
		t.Error("other-account row categorized by an account-bound rule")
	}
}
