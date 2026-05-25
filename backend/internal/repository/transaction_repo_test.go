package repository_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// loadRepoDotenv mirrors the helper in model/decimal_precision_test.go.
// `go test` sets cwd to the package dir, so config.Load()'s default
// search of ./.env and ../.env won't find the repo-root .env.
func loadRepoDotenv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	loadRepoDotenv()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no DATABASE_URL set; skipping integration test")
	}
	g, err := db.Open(url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx, g); err != nil {
		t.Skipf("db.Ping: %v; skipping integration test", err)
	}
	return g
}

// seedTxFixture builds a small, deterministic transaction set spanning two
// accounts and two categories so that filter dimensions are independent.
//
// Returns (userID, accountA, accountB, categoryX, categoryY, [tx ids]).
// All fixture rows belong to a freshly-seeded user; tests pass that userID
// into repo calls so they see only their own data.
func seedTxFixture(t *testing.T, g *gorm.DB) (int64, int64, int64, int64, int64, []int64) {
	t.Helper()
	ctx := context.Background()

	userID := seedTestUser(t, g)
	usdID := lookupUSDAssetID(t, g)

	accA := &model.Account{UserID: userID, Name: "fixture-A", InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD", PrimaryQuoteAssetID: usdID}
	accB := &model.Account{UserID: userID, Name: "fixture-B", InstitutionSlug: "fixture", AccountType: "credit_card", Currency: "USD", PrimaryQuoteAssetID: usdID}
	if err := g.WithContext(ctx).Create(accA).Error; err != nil {
		t.Fatalf("seed account A: %v", err)
	}
	if err := g.WithContext(ctx).Create(accB).Error; err != nil {
		t.Fatalf("seed account B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.Account{}, accA.ID)
		g.Unscoped().Delete(&model.Account{}, accB.ID)
	})

	catX := &model.Category{Name: "FixtureX", Slug: "fixture-x-" + time.Now().Format("150405.000000"), IsSystem: false}
	catY := &model.Category{Name: "FixtureY", Slug: "fixture-y-" + time.Now().Format("150405.000000"), IsSystem: false}
	if err := g.WithContext(ctx).Create(catX).Error; err != nil {
		t.Fatalf("seed cat X: %v", err)
	}
	if err := g.WithContext(ctx).Create(catY).Error; err != nil {
		t.Fatalf("seed cat Y: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.Category{}, catX.ID)
		g.Unscoped().Delete(&model.Category{}, catY.ID)
	})

	d := func(s string) time.Time {
		v, _ := time.Parse("2006-01-02", s)
		return v
	}
	desc := func(s string) *string { return &s }

	rows := []model.Transaction{
		{UserID: userID, AccountID: accA.ID, AssetID: usdID, CategoryID: &catX.ID, Amount: decimal.NewFromInt(-100),
			Description: desc("Whole Foods market"), MerchantName: desc("Whole Foods"),
			TransactionDate: d("2026-05-10"), Source: "manual"},
		{UserID: userID, AccountID: accA.ID, AssetID: usdID, CategoryID: &catY.ID, Amount: decimal.NewFromInt(-40),
			Description: desc("Shell gas"), MerchantName: desc("Shell"),
			TransactionDate: d("2026-05-12"), Source: "manual"},
		{UserID: userID, AccountID: accB.ID, AssetID: usdID, Amount: decimal.NewFromInt(-5),
			Description: desc("Coffee shop"), MerchantName: desc("Blue Bottle"),
			TransactionDate: d("2026-05-15"), Source: "manual"},
		{UserID: userID, AccountID: accB.ID, AssetID: usdID, CategoryID: &catX.ID, Amount: decimal.NewFromInt(25),
			Description: desc("Whole Foods returns"), MerchantName: desc("Whole Foods"),
			TransactionDate: d("2026-05-20"), Source: "manual"},
		{UserID: userID, AccountID: accA.ID, AssetID: usdID, Amount: decimal.NewFromInt(-1),
			Description: desc("Old transaction"), MerchantName: desc("Misc"),
			TransactionDate: d("2026-04-01"), Source: "manual"},
	}
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		if err := g.WithContext(ctx).Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed tx %d: %v", i, err)
		}
		ids = append(ids, rows[i].ID)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			g.Unscoped().Delete(&model.Transaction{}, id)
		}
	})

	return userID, accA.ID, accB.ID, catX.ID, catY.ID, ids
}

func TestTransactionRepository_List_Filters(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewTransactionRepository(g)
	userID, accA, accB, catX, _, ids := seedTxFixture(t, g)

	// Helper: assert that the returned rows are EXACTLY the expected fixture indices.
	// idsByIndex maps fixture row index (0..4) → tx id. Tests reference indices so
	// the assertions stay readable when fixtures are reordered.
	want := func(indices ...int) map[int64]struct{} {
		m := make(map[int64]struct{}, len(indices))
		for _, i := range indices {
			m[ids[i]] = struct{}{}
		}
		return m
	}

	cases := []struct {
		name      string
		filter    repository.TransactionFilter
		wantIDs   map[int64]struct{}
		wantTotal int64
		wantOrder []int // expected (newest-first) order by fixture index, optional
	}{
		{
			name:      "by account_id A",
			filter:    repository.TransactionFilter{AccountID: int64Ptr(accA)},
			wantIDs:   want(0, 1, 4),
			wantTotal: 3,
			wantOrder: []int{1, 0, 4}, // 2026-05-12, 2026-05-10, 2026-04-01
		},
		{
			name:      "by account_id B",
			filter:    repository.TransactionFilter{AccountID: int64Ptr(accB)},
			wantIDs:   want(2, 3),
			wantTotal: 2,
		},
		{
			name:      "by category_id X",
			filter:    repository.TransactionFilter{CategoryID: int64Ptr(catX)},
			wantIDs:   want(0, 3),
			wantTotal: 2,
		},
		{
			name:      "uncategorized only",
			filter:    repository.TransactionFilter{UncategorizedOnly: true, AccountID: nil},
			wantIDs:   nil, // checked below — we don't assert global count because other fixtures may exist
			wantTotal: -1,
		},
		{
			// Scoped to accB so other tests' rows in this date window don't pollute counts.
			// accB has rows on 5/15 (idx 2) and 5/20 (idx 3); only 5/15 falls in [5/11, 5/16].
			name:      "date range from 2026-05-11 to 2026-05-16 (scoped to accB)",
			filter:    repository.TransactionFilter{AccountID: int64Ptr(accB), From: timePtr("2026-05-11"), To: timePtr("2026-05-16")},
			wantIDs:   want(2),
			wantTotal: 1,
		},
		{
			// "Whole Foods returns" is a fixture-unique phrase, isolating us from
			// other smoke-test rows that may also use "Whole Foods".
			name:      "search exact 'Whole Foods returns' (fixture-unique)",
			filter:    repository.TransactionFilter{Search: "Whole Foods returns"},
			wantIDs:   want(3),
			wantTotal: 1,
		},
		{
			name:      "search via merchant_name 'blue bottle'",
			filter:    repository.TransactionFilter{Search: "Blue Bottle"},
			wantIDs:   want(2),
			wantTotal: 1,
		},
		{
			name:      "combined: account A + cat X",
			filter:    repository.TransactionFilter{AccountID: int64Ptr(accA), CategoryID: int64Ptr(catX)},
			wantIDs:   want(0),
			wantTotal: 1,
		},
		{
			name:      "limit clamped (-1 → default 50)",
			filter:    repository.TransactionFilter{AccountID: int64Ptr(accA), Limit: -1},
			wantIDs:   want(0, 1, 4),
			wantTotal: 3,
		},
		{
			name:      "limit clamped (300 → max 200)",
			filter:    repository.TransactionFilter{AccountID: int64Ptr(accA), Limit: 300},
			wantIDs:   want(0, 1, 4),
			wantTotal: 3,
		},
		{
			name:      "pagination: limit 1 offset 1 on accountA returns the middle row by date DESC",
			filter:    repository.TransactionFilter{AccountID: int64Ptr(accA), Limit: 1, Offset: 1},
			wantIDs:   want(0), // 2nd-newest of A is fixture index 0 (2026-05-10)
			wantTotal: 3,       // total ignores limit/offset
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, total, err := repo.List(context.Background(), userID, tc.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}

			// Special-case: "uncategorized only" can pick up rows from other tests
			// in the same DB. Just assert OUR uncategorized rows are present.
			if tc.filter.UncategorizedOnly {
				gotIDs := txIDSet(got)
				for _, idx := range []int{2, 4} {
					if _, ok := gotIDs[ids[idx]]; !ok {
						t.Errorf("expected uncategorized fixture row index %d (id %d) in results", idx, ids[idx])
					}
				}
				return
			}

			if tc.wantTotal >= 0 && total != tc.wantTotal {
				t.Errorf("total = %d, want %d", total, tc.wantTotal)
			}
			if tc.wantIDs != nil {
				gotIDs := txIDSet(got)
				if len(gotIDs) != len(tc.wantIDs) {
					t.Errorf("len(results) = %d, want %d (got ids %v, want %v)",
						len(gotIDs), len(tc.wantIDs), gotIDs, tc.wantIDs)
				}
				for id := range tc.wantIDs {
					if _, ok := gotIDs[id]; !ok {
						t.Errorf("missing expected id %d", id)
					}
				}
			}
			if tc.wantOrder != nil {
				if len(got) != len(tc.wantOrder) {
					t.Fatalf("ordering check: len(got)=%d, want %d", len(got), len(tc.wantOrder))
				}
				for i, idx := range tc.wantOrder {
					if got[i].ID != ids[idx] {
						t.Errorf("position %d: id %d, want %d (fixture index %d)",
							i, got[i].ID, ids[idx], idx)
					}
				}
			}
		})
	}
}

func TestTransactionRepository_List_ExcludesSoftDeleted(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewTransactionRepository(g)
	userID, accA, _, _, _, ids := seedTxFixture(t, g)

	// Soft-delete fixture row index 1 (account A, 2026-05-12).
	if err := repo.SoftDelete(context.Background(), userID, ids[1]); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	got, total, err := repo.List(context.Background(), userID, repository.TransactionFilter{AccountID: int64Ptr(accA)})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 (one soft-deleted should be excluded)", total)
	}
	gotIDs := txIDSet(got)
	if _, ok := gotIDs[ids[1]]; ok {
		t.Errorf("soft-deleted id %d appeared in list", ids[1])
	}
}

// TestTransactionRepository_List_TenantIsolation asserts that a different
// user's data is never returned — the multi-tenant rule per .claude/rules/testing.md.
func TestTransactionRepository_List_TenantIsolation(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewTransactionRepository(g)
	_, accA, _, _, _, ids := seedTxFixture(t, g)

	otherUserID := seedTestUser(t, g)

	// Same filter, different tenant — must see nothing.
	got, total, err := repo.List(context.Background(), otherUserID, repository.TransactionFilter{AccountID: int64Ptr(accA)})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 || len(got) != 0 {
		t.Errorf("other user got %d rows (total=%d); want 0", len(got), total)
	}
	// And GetByID must 404 (ErrNotFound).
	if _, err := repo.GetByID(context.Background(), otherUserID, ids[0]); err != repository.ErrNotFound {
		t.Errorf("GetByID across tenants: err = %v, want ErrNotFound", err)
	}
}

func int64Ptr(v int64) *int64 { return &v }

func timePtr(s string) *time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return &t
}

func txIDSet(ts []model.Transaction) map[int64]struct{} {
	out := make(map[int64]struct{}, len(ts))
	for _, t := range ts {
		out[t.ID] = struct{}{}
	}
	return out
}
