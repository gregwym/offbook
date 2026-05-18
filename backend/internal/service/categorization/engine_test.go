package categorization_test

import (
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

func ptr(s string) *string { return &s }

func TestCompile_SkipsInvalidRegexAndInactive(t *testing.T) {
	rules := []model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "WHOLEFDS", IsActive: true},
		{ID: 2, CategoryID: 20, MatchType: "regex", Pattern: "([unclosed", IsActive: true},
		{ID: 3, CategoryID: 30, MatchType: "contains", Pattern: "INACTIVE", IsActive: false},
		{ID: 4, CategoryID: 40, MatchType: "regex", Pattern: `^AMZN\s`, IsActive: true},
	}
	got := categorization.Compile(rules)
	if len(got) != 2 {
		t.Fatalf("Compile: got %d rules, want 2 (skip invalid regex + inactive)", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 4 {
		t.Errorf("unexpected order/ids: %+v", got)
	}
	if got[1].Re == nil {
		t.Errorf("regex rule should have non-nil Re")
	}
}

func TestCategorize_FirstMatchWins(t *testing.T) {
	// Repo returns priority DESC, id ASC; Compile preserves order. So the
	// first rule in the slice is the highest-priority one.
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "WHOLE", IsActive: true}, // high priority
		{ID: 2, CategoryID: 20, MatchType: "contains", Pattern: "FOODS", IsActive: true}, // would also match
	})

	d, ok := categorization.Categorize(rules, nil, ptr("WHOLE FOODS MARKET #123"), nil)
	if !ok {
		t.Fatal("expected match")
	}
	if d.RuleID != 1 || d.CategoryID != 10 {
		t.Errorf("got rule %d/cat %d, want 1/10 (higher priority)", d.RuleID, d.CategoryID)
	}
}

func TestCategorize_MatchTypes(t *testing.T) {
	cases := []struct {
		name      string
		rule      model.CategorizationRule
		desc      *string
		merchant  *string
		wantMatch bool
	}{
		{"contains case-insensitive",
			model.CategorizationRule{ID: 1, CategoryID: 9, MatchType: "contains", Pattern: "wholefds", IsActive: true},
			ptr("WHOLEFDS MARKET"), nil, true},
		{"contains no match",
			model.CategorizationRule{ID: 1, CategoryID: 9, MatchType: "contains", Pattern: "WHOLEFDS", IsActive: true},
			ptr("TRADER JOES"), nil, false},
		{"exact case-insensitive",
			model.CategorizationRule{ID: 1, CategoryID: 9, MatchType: "exact", Pattern: "amzn mktp", IsActive: true},
			ptr("AMZN MKTP"), nil, true},
		{"exact substring rejected",
			model.CategorizationRule{ID: 1, CategoryID: 9, MatchType: "exact", Pattern: "AMZN", IsActive: true},
			ptr("AMZN MKTP"), nil, false},
		{"regex matches",
			model.CategorizationRule{ID: 1, CategoryID: 9, MatchType: "regex", Pattern: `^AMZN\s`, IsActive: true},
			ptr("AMZN MKTP"), nil, true},
		{"regex no match",
			model.CategorizationRule{ID: 1, CategoryID: 9, MatchType: "regex", Pattern: `^AMZN\s`, IsActive: true},
			ptr("PRIME AMZN"), nil, false},
		{"matches merchant field",
			model.CategorizationRule{ID: 1, CategoryID: 9, MatchType: "contains", Pattern: "starbucks", IsActive: true},
			ptr("Random description"), ptr("Starbucks #4321"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := categorization.Compile([]model.CategorizationRule{tc.rule})
			_, ok := categorization.Categorize(rs, nil, tc.desc, tc.merchant)
			if ok != tc.wantMatch {
				t.Errorf("Categorize matched=%v, want %v", ok, tc.wantMatch)
			}
		})
	}
}

func TestCategorize_NoFieldsNoMatch(t *testing.T) {
	rs := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 9, MatchType: "contains", Pattern: "X", IsActive: true},
	})
	if _, ok := categorization.Categorize(rs, nil, nil, nil); ok {
		t.Error("expected no match with all-nil fields")
	}
	emptyStr := "   "
	if _, ok := categorization.Categorize(rs, nil, &emptyStr, nil); ok {
		t.Error("expected no match with whitespace-only field")
	}
}

func TestApply_MutatesTransaction(t *testing.T) {
	rs := categorization.Compile([]model.CategorizationRule{
		{ID: 7, CategoryID: 42, MatchType: "contains", Pattern: "WHOLE", IsActive: true},
	})
	tx := &model.Transaction{Description: ptr("WHOLE FOODS")}
	d, ok := categorization.Apply(tx, rs)
	if !ok {
		t.Fatal("expected match")
	}
	if d.RuleID != 7 || d.CategoryID != 42 {
		t.Errorf("decision = %+v, want rule 7 cat 42", d)
	}
	if tx.CategoryID == nil || *tx.CategoryID != 42 {
		t.Errorf("CategoryID = %v, want 42", tx.CategoryID)
	}
	if tx.CategorizationRuleID == nil || *tx.CategorizationRuleID != 7 {
		t.Errorf("CategorizationRuleID = %v, want 7", tx.CategorizationRuleID)
	}
	if tx.CategorizationMethod == nil || *tx.CategorizationMethod != "rule" {
		t.Errorf("CategorizationMethod = %v, want rule", tx.CategorizationMethod)
	}
}

func TestApply_NoMatchLeavesTransactionUntouched(t *testing.T) {
	rs := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 99, MatchType: "contains", Pattern: "ZZZZ", IsActive: true},
	})
	tx := &model.Transaction{Description: ptr("Coffee")}
	if _, ok := categorization.Apply(tx, rs); ok {
		t.Fatal("expected no match")
	}
	if tx.CategoryID != nil || tx.CategorizationRuleID != nil || tx.CategorizationMethod != nil {
		t.Errorf("Apply mutated tx on no-match: %+v", tx)
	}
}
