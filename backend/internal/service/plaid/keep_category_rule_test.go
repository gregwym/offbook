package plaid_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// TestMapPlaidTransaction_KeepCategoryRuleKeepsPFCDefault (#553): a matched
// rule without a category tags the row, and the Plaid PFC default still
// sets the category.
func TestMapPlaidTransaction_KeepCategoryRuleKeepsPFCDefault(t *testing.T) {
	g := openPlaidTestDB(t)
	mapper, err := plaidsvc.NewCategoryMapper(context.Background(), repository.NewPlaidCategoryMapRepository(g))
	if err != nil {
		t.Fatalf("NewCategoryMapper: %v", err)
	}
	pfcCat, ok := mapper.MapPlaidCategory("GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_GIFTS_AND_NOVELTIES")
	if !ok {
		t.Fatal("seed map has no GENERAL_MERCHANDISE_GIFTS_AND_NOVELTIES entry")
	}
	rules := categorization.Compile([]model.CategorizationRule{
		{ID: 1, MatchType: "contains", Pattern: "UBER", IsActive: true, AddTags: model.Tags{"reimbursable"}},
	})
	in := plaidsvc.PlaidTransaction{
		PlaidTransactionID: "ptx-keep-cat", PlaidAccountID: "pacct", Amount: decimal.RequireFromString("23.40"),
		Currency: "USD", Name: "UBER GIFT CARD", Date: "2026-05-16",
		PFCPrimary: "GENERAL_MERCHANDISE", PFCDetailed: "GENERAL_MERCHANDISE_GIFTS_AND_NOVELTIES",
	}
	got, err := plaidsvc.MapPlaidTransaction(in, 7, 42, 1, mapper, rules)
	if err != nil {
		t.Fatalf("MapPlaidTransaction: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != pfcCat {
		t.Errorf("category_id = %v, want PFC default %d", got.CategoryID, pfcCat)
	}
	if got.CategorizationMethod == nil || *got.CategorizationMethod != plaidsvc.CategorizationMethodPlaidDefault {
		t.Errorf("categorization_method = %v, want plaid_default", got.CategorizationMethod)
	}
	if got.CategorizationRuleID != nil {
		t.Errorf("categorization_rule_id = %d, want nil", *got.CategorizationRuleID)
	}
	if strings.Join(got.Tags, ",") != "reimbursable" {
		t.Errorf("tags = %v, want [reimbursable]", got.Tags)
	}
}
