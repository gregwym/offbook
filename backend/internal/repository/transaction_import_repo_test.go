package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// seedImportAccount creates a USD checking account for the given user and
// returns its id, registering cleanup (transactions first, then account).
func seedImportAccount(t *testing.T, g *gorm.DB, userID int64) int64 {
	t.Helper()
	usdID := lookupUSDAssetID(t, g)
	acc := &model.Account{
		UserID:              userID,
		Name:                "import-repo-" + time.Now().Format("150405.000000000"),
		InstitutionSlug:     "fixture",
		AccountType:         "checking",
		Currency:            "USD",
		PrimaryQuoteAssetID: usdID,
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, acc.ID) })
	t.Cleanup(func() { g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{}) })
	return acc.ID
}

func strp(s string) *string { return &s }

// TestExistingExternalIDs_UserAndAccountScoped proves the CSV dedup lookup is
// tenant- and account-scoped: it never reports another user's (or another
// account's) external_id as already-present.
func TestExistingExternalIDs_UserAndAccountScoped(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewTransactionRepository(g)
	ctx := context.Background()
	usd := lookupUSDAssetID(t, g)

	userA := seedTestUser(t, g)
	userB := seedTestUser(t, g)
	accA := seedImportAccount(t, g, userA)
	accB := seedImportAccount(t, g, userB)

	// User A's account A has csv:shared. User B's account B also has csv:shared
	// — same external_id string, different account/user namespace.
	seed := []model.Transaction{
		{UserID: userA, AccountID: accA, AssetID: usd, Amount: decimal.NewFromInt(-10),
			TransactionDate: time.Now(), Source: "csv", ExternalID: strp("csv:shared")},
		{UserID: userB, AccountID: accB, AssetID: usd, Amount: decimal.NewFromInt(-10),
			TransactionDate: time.Now(), Source: "csv", ExternalID: strp("csv:shared")},
	}
	if _, err := repo.ImportBatch(ctx, seed); err != nil {
		t.Fatalf("seed ImportBatch: %v", err)
	}

	// A asking about account A sees csv:shared, not csv:absent.
	got, err := repo.ExistingExternalIDs(ctx, userA, accA, []string{"csv:shared", "csv:absent"})
	if err != nil {
		t.Fatalf("ExistingExternalIDs: %v", err)
	}
	if _, ok := got["csv:shared"]; !ok || len(got) != 1 {
		t.Errorf("user A / account A = %v, want only csv:shared", got)
	}

	// A asking about account B (not theirs) sees nothing — account scoping.
	got, err = repo.ExistingExternalIDs(ctx, userA, accB, []string{"csv:shared"})
	if err != nil {
		t.Fatalf("ExistingExternalIDs (cross-account): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("user A / account B = %v, want empty (not their account)", got)
	}

	// Empty input → empty set, no query error.
	got, err = repo.ExistingExternalIDs(ctx, userA, accA, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("empty input = (%v, %v), want (empty, nil)", got, err)
	}
}

// TestImportBatch_DedupsOnExternalID proves ON CONFLICT (account_id,
// external_id) DO NOTHING: a re-insert of the same key inserts zero rows.
func TestImportBatch_DedupsOnExternalID(t *testing.T) {
	g := openTestDB(t)
	repo := repository.NewTransactionRepository(g)
	ctx := context.Background()
	usd := lookupUSDAssetID(t, g)
	userID := seedTestUser(t, g)
	accID := seedImportAccount(t, g, userID)

	row := func() model.Transaction {
		return model.Transaction{
			UserID: userID, AccountID: accID, AssetID: usd, Amount: decimal.NewFromInt(-7),
			TransactionDate: time.Now(), Source: "csv", ExternalID: strp("csv:dup"),
		}
	}

	n, err := repo.ImportBatch(ctx, []model.Transaction{row()})
	if err != nil || n != 1 {
		t.Fatalf("first insert = (%d, %v), want (1, nil)", n, err)
	}
	// Same external_id again → conflict → zero inserted.
	n, err = repo.ImportBatch(ctx, []model.Transaction{row()})
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if n != 0 {
		t.Errorf("re-insert inserted %d rows, want 0 (dedup)", n)
	}
}
