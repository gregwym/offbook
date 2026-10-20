package categorization_test

import (
	"testing"
	"time"

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

	d, ok := categorization.Categorize(rules, 0, nil, ptr("WHOLE FOODS MARKET #123"), nil)
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
			_, ok := categorization.Categorize(rs, 0, nil, tc.desc, tc.merchant)
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
	if _, ok := categorization.Categorize(rs, 0, nil, nil, nil); ok {
		t.Error("expected no match with all-nil fields")
	}
	emptyStr := "   "
	if _, ok := categorization.Categorize(rs, 0, nil, &emptyStr, nil); ok {
		t.Error("expected no match with whitespace-only field")
	}
}

func TestCategorize_AssetIDFilter(t *testing.T) {
	aapl := int64(101)
	other := int64(202)
	rs := categorization.Compile([]model.CategorizationRule{
		// Asset-bound rule with no pattern — matches any AAPL leg.
		{ID: 1, CategoryID: 11, MatchType: "contains", Pattern: "", AssetID: &aapl, IsActive: true},
		// Asset-agnostic rule — matches anything with "DIVIDEND".
		{ID: 2, CategoryID: 22, MatchType: "contains", Pattern: "DIVIDEND", IsActive: true},
	})

	// AAPL leg with no text → first rule fires on asset alone.
	d, ok := categorization.Categorize(rs, aapl, nil, ptr("Bought 10 @ 150"), nil)
	if !ok || d.RuleID != 1 {
		t.Errorf("AAPL leg: got rule=%d match=%v, want rule=1 match=true", d.RuleID, ok)
	}
	// Non-AAPL leg with no matching text → no match (rule 1 filters on asset).
	if _, ok := categorization.Categorize(rs, other, nil, ptr("Misc"), nil); ok {
		t.Error("non-AAPL leg should NOT match an AAPL-bound rule")
	}
	// Non-AAPL leg with matching text → rule 2 (asset-agnostic) fires.
	d, ok = categorization.Categorize(rs, other, nil, ptr("Quarterly DIVIDEND"), nil)
	if !ok || d.RuleID != 2 {
		t.Errorf("agnostic rule: got rule=%d match=%v, want 2/true", d.RuleID, ok)
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

func TestApply_RenameMerchant(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "AMZN", IsActive: true, RenameMerchant: ptr("  Amazon ")},
		{ID: 2, CategoryID: 20, MatchType: "contains", Pattern: "SHELL", IsActive: true, RenameMerchant: ptr("   ")},
	})

	t.Run("winning rule renames", func(t *testing.T) {
		tx := &model.Transaction{Description: ptr("AMZN MKTP US*2K4"), MerchantName: ptr("AMZN Mktp")}
		d, ok := categorization.Apply(tx, rules)
		if !ok || d.RuleID != 1 {
			t.Fatalf("Apply: ok=%v rule=%d, want rule 1", ok, d.RuleID)
		}
		if tx.MerchantName == nil || *tx.MerchantName != "Amazon" {
			t.Errorf("merchant = %v, want Amazon (trimmed)", tx.MerchantName)
		}
		if !d.SameAs(tx) {
			t.Error("SameAs after Apply = false, want true (re-apply is a no-op)")
		}
	})
	t.Run("blank rename leaves merchant alone", func(t *testing.T) {
		tx := &model.Transaction{Description: ptr("SHELL OIL 123"), MerchantName: ptr("Shell")}
		d, ok := categorization.Apply(tx, rules)
		if !ok || d.RenameMerchant != nil {
			t.Fatalf("Apply: ok=%v rename=%v, want match without rename", ok, d.RenameMerchant)
		}
		if *tx.MerchantName != "Shell" {
			t.Errorf("merchant = %q, want Shell", *tx.MerchantName)
		}
	})
	t.Run("SameAs sees a stale merchant", func(t *testing.T) {
		tx := &model.Transaction{Description: ptr("AMZN MKTP")}
		d, _ := categorization.Apply(tx, rules)
		tx.MerchantName = ptr("AMZN Mktp")
		if d.SameAs(tx) {
			t.Error("SameAs = true with a different merchant, want false")
		}
	})
}

func TestApply_HideMatches(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "SWEEP", IsActive: true, HideMatches: true},
		{ID: 2, CategoryID: 20, MatchType: "contains", Pattern: "SHELL", IsActive: true},
	})

	t.Run("winning rule hides a flow row", func(t *testing.T) {
		tx := &model.Transaction{Kind: model.KindFlow, Description: ptr("CASH SWEEP TO MMF")}
		d, ok := categorization.Apply(tx, rules)
		if !ok || d.RuleID != 1 || !d.Hide {
			t.Fatalf("Apply: ok=%v rule=%d hide=%v, want rule 1 hiding", ok, d.RuleID, d.Hide)
		}
		if tx.HiddenAt == nil {
			t.Fatal("HiddenAt = nil, want set")
		}
		if !d.SameAs(tx) {
			t.Error("SameAs after Apply = false, want true (re-apply is a no-op)")
		}
	})
	t.Run("already hidden keeps its timestamp", func(t *testing.T) {
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		tx := &model.Transaction{Description: ptr("SWEEP"), HiddenAt: &at}
		categorization.Apply(tx, rules)
		if tx.HiddenAt == nil || !tx.HiddenAt.Equal(at) {
			t.Errorf("HiddenAt = %v, want %v kept", tx.HiddenAt, at)
		}
	})
	t.Run("non-flow rows are never hidden", func(t *testing.T) {
		tx := &model.Transaction{Kind: model.KindTradeLeg, Description: ptr("SWEEP")}
		d, ok := categorization.Apply(tx, rules)
		if !ok {
			t.Fatal("Apply: no match, want rule 1")
		}
		if tx.HiddenAt != nil || d.HideChanged(tx) {
			t.Errorf("trade leg hidden: HiddenAt=%v", tx.HiddenAt)
		}
	})
	t.Run("rule without hide leaves the row visible", func(t *testing.T) {
		tx := &model.Transaction{Description: ptr("SHELL OIL")}
		categorization.Apply(tx, rules)
		if tx.HiddenAt != nil {
			t.Errorf("HiddenAt = %v, want nil", tx.HiddenAt)
		}
	})
	t.Run("SameAs sees a row the user unhid", func(t *testing.T) {
		tx := &model.Transaction{Description: ptr("SWEEP")}
		d, _ := categorization.Apply(tx, rules)
		tx.HiddenAt = nil
		if d.SameAs(tx) {
			t.Error("SameAs = true on a visible row, want false")
		}
	})
}

func TestApply_MarkReviewed(t *testing.T) {
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, CategoryID: 10, MatchType: "contains", Pattern: "PAYROLL", IsActive: true, MarkReviewed: true},
		{ID: 2, CategoryID: 20, MatchType: "contains", Pattern: "SHELL", IsActive: true},
	})

	t.Run("winning rule marks a flow row reviewed", func(t *testing.T) {
		tx := &model.Transaction{Kind: model.KindFlow, Description: ptr("ACME PAYROLL")}
		d, ok := categorization.Apply(tx, rules)
		if !ok || d.RuleID != 1 || !d.MarkReviewed {
			t.Fatalf("Apply: ok=%v rule=%d mark=%v, want rule 1 marking reviewed", ok, d.RuleID, d.MarkReviewed)
		}
		if tx.ReviewedAt == nil {
			t.Fatal("ReviewedAt = nil, want set")
		}
		if tx.HiddenAt != nil {
			t.Errorf("HiddenAt = %v, want nil (review mark doesn't hide)", tx.HiddenAt)
		}
		if !d.SameAs(tx) {
			t.Error("SameAs after Apply = false, want true (re-apply is a no-op)")
		}
	})
	t.Run("already reviewed keeps its timestamp", func(t *testing.T) {
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		tx := &model.Transaction{Description: ptr("PAYROLL"), ReviewedAt: &at}
		categorization.Apply(tx, rules)
		if tx.ReviewedAt == nil || !tx.ReviewedAt.Equal(at) {
			t.Errorf("ReviewedAt = %v, want %v kept", tx.ReviewedAt, at)
		}
	})
	t.Run("non-flow rows are never marked", func(t *testing.T) {
		tx := &model.Transaction{Kind: model.KindTradeLeg, Description: ptr("PAYROLL")}
		d, ok := categorization.Apply(tx, rules)
		if !ok {
			t.Fatal("Apply: no match, want rule 1")
		}
		if tx.ReviewedAt != nil || d.ReviewChanged(tx) {
			t.Errorf("trade leg marked reviewed: ReviewedAt=%v", tx.ReviewedAt)
		}
	})
	t.Run("rule without the mark leaves the row unreviewed", func(t *testing.T) {
		tx := &model.Transaction{Description: ptr("SHELL OIL")}
		categorization.Apply(tx, rules)
		if tx.ReviewedAt != nil {
			t.Errorf("ReviewedAt = %v, want nil", tx.ReviewedAt)
		}
	})
	t.Run("SameAs sees a row the user sent back to review", func(t *testing.T) {
		tx := &model.Transaction{Description: ptr("PAYROLL")}
		d, _ := categorization.Apply(tx, rules)
		tx.ReviewedAt = nil
		if d.SameAs(tx) {
			t.Error("SameAs = true on an unreviewed row, want false")
		}
	})
}
