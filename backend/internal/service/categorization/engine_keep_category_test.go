package categorization_test

import (
	"strings"
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// TestApply_KeepCategoryRule (#553): a rule without a category takes only its
// other actions. It never writes category_id, categorization_method or
// categorization_rule_id, and it still wins first-match, so a later
// categorizing rule for the same merchant does not run on the row.
func TestApply_KeepCategoryRule(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, MatchType: "contains", Pattern: "UBER", IsActive: true, AddTags: model.Tags{"reimbursable"}},
		{ID: 2, CategoryID: new(int64(20)), MatchType: "contains", Pattern: "UBER", IsActive: true},
	})
	plaid := "plaid_default"

	cases := []struct {
		name       string
		tx         *model.Transaction
		wantCat    *int64
		wantMethod *string
	}{
		{"uncategorized row stays uncategorized", &model.Transaction{Description: ptr("UBER TRIP")}, nil, nil},
		{"plaid default category is kept", &model.Transaction{Description: ptr("UBER TRIP"), CategoryID: new(int64(7)), CategorizationMethod: &plaid}, new(int64(7)), &plaid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := categorization.Apply(tc.tx, rules)
			if !ok || d.RuleID != 1 {
				t.Fatalf("Apply: ok=%v rule=%d, want rule 1 (first match wins)", ok, d.RuleID)
			}
			if d.SetsCategory() {
				t.Error("SetsCategory = true, want false")
			}
			if (tc.tx.CategoryID == nil) != (tc.wantCat == nil) || (tc.wantCat != nil && *tc.tx.CategoryID != *tc.wantCat) {
				t.Errorf("CategoryID = %v, want %v", tc.tx.CategoryID, tc.wantCat)
			}
			if (tc.tx.CategorizationMethod == nil) != (tc.wantMethod == nil) {
				t.Errorf("CategorizationMethod = %v, want %v", tc.tx.CategorizationMethod, tc.wantMethod)
			}
			if tc.tx.CategorizationRuleID != nil {
				t.Errorf("CategorizationRuleID = %d, want nil", *tc.tx.CategorizationRuleID)
			}
			if strings.Join(tc.tx.Tags, ",") != "reimbursable" {
				t.Errorf("Tags = %v, want [reimbursable]", tc.tx.Tags)
			}
			if d.CategoryChanged(tc.tx) {
				t.Error("CategoryChanged = true, want false")
			}
			if !d.SameAs(tc.tx) {
				t.Error("SameAs after Apply = false, want true (re-apply is a no-op)")
			}
		})
	}

	t.Run("untagged row is not SameAs", func(t *testing.T) {
		d, _ := categorization.Match(rules, &model.Transaction{Description: ptr("UBER")})
		if d.SameAs(&model.Transaction{Description: ptr("UBER")}) {
			t.Error("SameAs = true before the tag is added, want false")
		}
	})
}
