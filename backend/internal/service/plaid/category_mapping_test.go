package plaid_test

import (
	"context"
	"testing"

	"github.com/gregwym/offbook/backend/internal/repository"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// fakeMapRepo is an in-memory PlaidCategoryMapRepository for unit tests.
// Real DB is exercised separately in the integration test.
type fakeMapRepo struct {
	rows []repository.PlaidCategoryMapping
}

func (r *fakeMapRepo) LoadAll(context.Context) ([]repository.PlaidCategoryMapping, error) {
	return r.rows, nil
}

func TestCategoryMapper_LookupTable(t *testing.T) {
	repo := &fakeMapRepo{rows: []repository.PlaidCategoryMapping{
		{PlaidPrimary: "FOOD_AND_DRINK", PlaidDetailed: "GROCERIES", CategoryID: 100},
		{PlaidPrimary: "FOOD_AND_DRINK", PlaidDetailed: "RESTAURANT", CategoryID: 101},
		{PlaidPrimary: "TRANSPORTATION", PlaidDetailed: "GAS", CategoryID: 200},
		{PlaidPrimary: "TRANSPORTATION", PlaidDetailed: "TAXIS_AND_RIDE_SHARES", CategoryID: 201},
		{PlaidPrimary: "INCOME", PlaidDetailed: "WAGES", CategoryID: 300},
		{PlaidPrimary: "BANK_FEES", PlaidDetailed: "OVERDRAFT_FEES", CategoryID: 400},
	}}
	m, err := plaidsvc.NewCategoryMapper(context.Background(), repo)
	if err != nil {
		t.Fatalf("NewCategoryMapper: %v", err)
	}
	if m.Size() != 6 {
		t.Errorf("Size = %d, want 6", m.Size())
	}

	cases := []struct {
		name     string
		primary  string
		detailed string
		wantID   int64
		wantOK   bool
	}{
		{"groceries", "FOOD_AND_DRINK", "GROCERIES", 100, true},
		{"restaurant", "FOOD_AND_DRINK", "RESTAURANT", 101, true},
		{"gas", "TRANSPORTATION", "GAS", 200, true},
		{"rideshare", "TRANSPORTATION", "TAXIS_AND_RIDE_SHARES", 201, true},
		{"wages", "INCOME", "WAGES", 300, true},
		{"overdraft", "BANK_FEES", "OVERDRAFT_FEES", 400, true},

		// Unmapped — Plaid does have these PFCs, we just didn't seed them.
		{"unmapped detailed", "FOOD_AND_DRINK", "BEER_WINE_AND_LIQUOR", 0, false},
		{"unknown primary", "MYSTERY", "RESTAURANT", 0, false},
		// Edge cases: empty inputs never match (defensive — Plaid may omit
		// PFC on a stray transaction).
		{"empty primary", "", "GROCERIES", 0, false},
		{"empty detailed", "FOOD_AND_DRINK", "", 0, false},
		{"both empty", "", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := m.MapPlaidCategory(tc.primary, tc.detailed)
			if gotID != tc.wantID || gotOK != tc.wantOK {
				t.Errorf("(%q,%q) → (%d, %v), want (%d, %v)",
					tc.primary, tc.detailed, gotID, gotOK, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestCategoryMapper_NilSafe(t *testing.T) {
	var m *plaidsvc.CategoryMapper
	if id, ok := m.MapPlaidCategory("FOOD_AND_DRINK", "GROCERIES"); ok || id != 0 {
		t.Errorf("nil receiver should return (0, false), got (%d, %v)", id, ok)
	}
	if m.Size() != 0 {
		t.Errorf("nil receiver Size = %d, want 0", m.Size())
	}
}

func TestCategoryMapper_EmptyLoad(t *testing.T) {
	m, err := plaidsvc.NewCategoryMapper(context.Background(), &fakeMapRepo{})
	if err != nil {
		t.Fatalf("NewCategoryMapper: %v", err)
	}
	if m.Size() != 0 {
		t.Errorf("Size = %d, want 0 for empty repo", m.Size())
	}
	if _, ok := m.MapPlaidCategory("FOOD_AND_DRINK", "GROCERIES"); ok {
		t.Error("empty mapper should not return a hit")
	}
}
