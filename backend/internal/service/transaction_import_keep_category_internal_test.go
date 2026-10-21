package service

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// TestBuildImportTxn_Rules (#553): statement import runs the rules through
// Match, so a keep-category rule tags the row and leaves it uncategorized,
// while a categorizing rule sets the category with the rule as provenance.
func TestBuildImportTxn_Rules(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, MatchType: "contains", Pattern: "UBER", IsActive: true, AddTags: model.Tags{"reimbursable"}},
		{ID: 2, CategoryID: new(int64(20)), MatchType: "contains", Pattern: "SHELL", IsActive: true},
	})
	acct := &model.Account{ID: 5, PrimaryQuoteAssetID: 1}
	cases := []struct {
		desc     string
		wantCat  *int64
		wantRule *int64
		wantTags string
	}{
		{"UBER TRIP", nil, nil, "reimbursable"},
		{"SHELL OIL", new(int64(20)), new(int64(2)), ""},
		{"COFFEE", nil, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			row := ingestion.ParsedRow{Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Amount: decimal.RequireFromString("-12.34"), Description: tc.desc}
			got := buildImportTxn(7, acct, "csv", row, "eid", rules, nil)
			if (got.CategoryID == nil) != (tc.wantCat == nil) || (tc.wantCat != nil && *got.CategoryID != *tc.wantCat) {
				t.Errorf("category_id = %v, want %v", got.CategoryID, tc.wantCat)
			}
			if (got.CategorizationRuleID == nil) != (tc.wantRule == nil) || (tc.wantRule != nil && *got.CategorizationRuleID != *tc.wantRule) {
				t.Errorf("categorization_rule_id = %v, want %v", got.CategorizationRuleID, tc.wantRule)
			}
			if tc.wantCat == nil && got.CategorizationMethod != nil {
				t.Errorf("categorization_method = %q, want nil", *got.CategorizationMethod)
			}
			if strings.Join(got.Tags, ",") != tc.wantTags {
				t.Errorf("tags = %v, want %q", got.Tags, tc.wantTags)
			}
			if !got.Amount.Equal(decimal.RequireFromString("-12.34")) {
				t.Errorf("amount = %s, want -12.34", got.Amount)
			}
		})
	}
}
