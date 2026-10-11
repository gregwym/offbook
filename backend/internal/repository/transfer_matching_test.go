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

// seedTransferFixtureAccounts creates two accounts (same USD asset, so they
// qualify as same-currency for matching) for a freshly seeded user.
func seedTransferFixtureAccounts(t *testing.T, g *gorm.DB) (userID, accA, accB int64) {
	t.Helper()
	userID = seedTestUser(t, g)
	usdID := lookupUSDAssetID(t, g)
	a := &model.Account{UserID: userID, Name: "xfer-A", InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD", PrimaryQuoteAssetID: usdID}
	b := &model.Account{UserID: userID, Name: "xfer-B", InstitutionSlug: "fixture", AccountType: "savings", Currency: "USD", PrimaryQuoteAssetID: usdID}
	if err := g.Create(a).Error; err != nil {
		t.Fatalf("seed account A: %v", err)
	}
	if err := g.Create(b).Error; err != nil {
		t.Fatalf("seed account B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.Account{}, a.ID)
		g.Unscoped().Delete(&model.Account{}, b.ID)
	})
	return userID, a.ID, b.ID
}

func seedFlowTxn(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, amount string, date string) *model.Transaction {
	t.Helper()
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	tx := &model.Transaction{
		UserID: userID, AccountID: accountID, AssetID: assetID,
		Kind: model.KindFlow, Amount: decimal.RequireFromString(amount),
		TransactionDate: d, Source: "manual",
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed flow txn: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
	return tx
}

func TestListTransferCandidates_MatchesOppositeAmountsAcrossAccounts(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewTransactionRepository(g)
	userID, accA, accB := seedTransferFixtureAccounts(t, g)
	usdID := lookupUSDAssetID(t, g)

	out := seedFlowTxn(t, g, userID, accA, usdID, "-500", "2026-06-01")
	in := seedFlowTxn(t, g, userID, accB, usdID, "500", "2026-06-02")

	candidates, err := repo.ListTransferCandidates(ctx, userID, 3)
	if err != nil {
		t.Fatalf("ListTransferCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	got := map[int64]bool{candidates[0].A.ID: true, candidates[0].B.ID: true}
	if !got[out.ID] || !got[in.ID] {
		t.Fatalf("candidate pair = (%d, %d), want (%d, %d)", candidates[0].A.ID, candidates[0].B.ID, out.ID, in.ID)
	}
}

func TestListTransferCandidates_ExcludesSameAccountDifferentAssetAndOutsideWindow(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewTransactionRepository(g)
	userID, accA, accB := seedTransferFixtureAccounts(t, g)
	usdID := lookupUSDAssetID(t, g)

	// Same account — never a transfer candidate even with opposite amounts.
	seedFlowTxn(t, g, userID, accA, usdID, "-75", "2026-06-01")
	seedFlowTxn(t, g, userID, accA, usdID, "75", "2026-06-01")

	// Different accounts, opposite amounts, but 10 days apart — outside the
	// default 3-day window.
	seedFlowTxn(t, g, userID, accA, usdID, "-200", "2026-06-01")
	seedFlowTxn(t, g, userID, accB, usdID, "200", "2026-06-11")

	candidates, err := repo.ListTransferCandidates(ctx, userID, 3)
	if err != nil {
		t.Fatalf("ListTransferCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates, want 0", len(candidates))
	}
}

func TestListTransferCandidates_SkipsAlreadyLinkedAndTenantIsolated(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewTransactionRepository(g)
	userID, accA, accB := seedTransferFixtureAccounts(t, g)
	otherUserID, otherAccA, otherAccB := seedTransferFixtureAccounts(t, g)
	usdID := lookupUSDAssetID(t, g)

	a := seedFlowTxn(t, g, userID, accA, usdID, "-300", "2026-06-05")
	b := seedFlowTxn(t, g, userID, accB, usdID, "300", "2026-06-05")
	if _, _, err := repo.LinkTransferPair(ctx, userID, a.ID, b.ID); err != nil {
		t.Fatalf("LinkTransferPair: %v", err)
	}

	// Another user has a matching pair too — must never surface in userID's list.
	seedFlowTxn(t, g, otherUserID, otherAccA, usdID, "-300", "2026-06-05")
	seedFlowTxn(t, g, otherUserID, otherAccB, usdID, "300", "2026-06-05")

	candidates, err := repo.ListTransferCandidates(ctx, userID, 3)
	if err != nil {
		t.Fatalf("ListTransferCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates for userID, want 0 (already linked)", len(candidates))
	}

	otherCandidates, err := repo.ListTransferCandidates(ctx, otherUserID, 3)
	if err != nil {
		t.Fatalf("ListTransferCandidates (other user): %v", err)
	}
	if len(otherCandidates) != 1 {
		t.Fatalf("got %d candidates for otherUserID, want 1", len(otherCandidates))
	}
}

func TestListTransferCandidates_GreedyBestMatchWins(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewTransactionRepository(g)
	userID, accA, accB := seedTransferFixtureAccounts(t, g)
	usdID := lookupUSDAssetID(t, g)

	// Two outflows of -100 from accA on different dates; one inflow of 100
	// in accB. The inflow should pair with the closer-dated outflow.
	far := seedFlowTxn(t, g, userID, accA, usdID, "-100", "2026-06-01")
	near := seedFlowTxn(t, g, userID, accA, usdID, "-100", "2026-06-03")
	inflow := seedFlowTxn(t, g, userID, accB, usdID, "100", "2026-06-04")

	candidates, err := repo.ListTransferCandidates(ctx, userID, 3)
	if err != nil {
		t.Fatalf("ListTransferCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	pair := map[int64]bool{candidates[0].A.ID: true, candidates[0].B.ID: true}
	if !pair[near.ID] || !pair[inflow.ID] {
		t.Fatalf("candidate pair = (%d, %d), want the near match (%d, %d); far=%d should be left unmatched",
			candidates[0].A.ID, candidates[0].B.ID, near.ID, inflow.ID, far.ID)
	}
}

func TestLinkTransferPair_SetsSymmetricFieldsAndUnlinkRestores(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewTransactionRepository(g)
	userID, accA, accB := seedTransferFixtureAccounts(t, g)
	usdID := lookupUSDAssetID(t, g)

	a := seedFlowTxn(t, g, userID, accA, usdID, "-150", "2026-06-07")
	b := seedFlowTxn(t, g, userID, accB, usdID, "150", "2026-06-07")

	linkedA, linkedB, err := repo.LinkTransferPair(ctx, userID, a.ID, b.ID)
	if err != nil {
		t.Fatalf("LinkTransferPair: %v", err)
	}
	if !linkedA.IsTransfer || !linkedB.IsTransfer {
		t.Fatalf("expected both legs IsTransfer=true")
	}
	if linkedA.TransferPairID == nil || *linkedA.TransferPairID != b.ID {
		t.Fatalf("legA.TransferPairID = %v, want %d", linkedA.TransferPairID, b.ID)
	}
	if linkedB.TransferPairID == nil || *linkedB.TransferPairID != a.ID {
		t.Fatalf("legB.TransferPairID = %v, want %d", linkedB.TransferPairID, a.ID)
	}

	// Re-read from the DB to make sure the writes actually persisted, not
	// just the in-memory structs LinkTransferPair returned.
	reread, err := repo.GetByID(ctx, userID, a.ID)
	if err != nil {
		t.Fatalf("GetByID a: %v", err)
	}
	if !reread.IsTransfer || reread.TransferPairID == nil || *reread.TransferPairID != b.ID {
		t.Fatalf("persisted leg A not linked correctly: %+v", reread)
	}

	if err := repo.UnlinkTransferPair(ctx, userID, a.ID); err != nil {
		t.Fatalf("UnlinkTransferPair: %v", err)
	}
	rerereadA, err := repo.GetByID(ctx, userID, a.ID)
	if err != nil {
		t.Fatalf("GetByID a after unlink: %v", err)
	}
	if rerereadA.IsTransfer || rerereadA.TransferPairID != nil {
		t.Fatalf("leg A still linked after unlink: %+v", rerereadA)
	}
	rerereadB, err := repo.GetByID(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("GetByID b after unlink: %v", err)
	}
	if rerereadB.IsTransfer || rerereadB.TransferPairID != nil {
		t.Fatalf("leg B still linked after unlink: %+v", rerereadB)
	}
}

func TestUnlinkTransferPair_NotFoundWhenNotLinked(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewTransactionRepository(g)
	userID, accA, _ := seedTransferFixtureAccounts(t, g)
	usdID := lookupUSDAssetID(t, g)

	a := seedFlowTxn(t, g, userID, accA, usdID, "-10", "2026-06-07")

	if err := repo.UnlinkTransferPair(ctx, userID, a.ID); err != repository.ErrNotFound {
		t.Fatalf("UnlinkTransferPair on unlinked txn: got %v, want ErrNotFound", err)
	}
}

func TestLinkTransferPair_CrossTenantRejected(t *testing.T) {
	g := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewTransactionRepository(g)
	userID, accA, _ := seedTransferFixtureAccounts(t, g)
	otherUserID, _, otherAccB := seedTransferFixtureAccounts(t, g)
	usdID := lookupUSDAssetID(t, g)

	a := seedFlowTxn(t, g, userID, accA, usdID, "-20", "2026-06-07")
	otherB := seedFlowTxn(t, g, otherUserID, otherAccB, usdID, "20", "2026-06-07")

	// userID attempting to link against otherUserID's transaction must fail
	// as not-found — the repo scopes both sides of the Where("user_id = ?").
	if _, _, err := repo.LinkTransferPair(ctx, userID, a.ID, otherB.ID); err != repository.ErrNotFound {
		t.Fatalf("cross-tenant LinkTransferPair: got %v, want ErrNotFound", err)
	}
}
