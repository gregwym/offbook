package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// newTransferFixture seeds a user with two same-currency accounts and
// returns the service under test plus both account ids.
func newTransferFixture(t *testing.T) (svc *service.TransferMatchingService, userID, accA, accB int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)
	accAModel := &model.Account{UserID: userID, Name: "Xfer-A-" + time.Now().Format("150405.000000"), InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD"}
	accBModel := &model.Account{UserID: userID, Name: "Xfer-B-" + time.Now().Format("150405.000000"), InstitutionSlug: "fixture", AccountType: "savings", Currency: "USD"}
	if err := g.Create(accAModel).Error; err != nil {
		t.Fatalf("seed account A: %v", err)
	}
	if err := g.Create(accBModel).Error; err != nil {
		t.Fatalf("seed account B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id IN (?, ?)", accAModel.ID, accBModel.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, accAModel.ID)
		g.Unscoped().Delete(&model.Account{}, accBModel.ID)
	})
	svc = service.NewTransferMatchingService(repository.NewTransactionRepository(g))
	return svc, userID, accAModel.ID, accBModel.ID, g
}

func seedTransferTxn(t *testing.T, g *gorm.DB, userID, accountID int64, kind, amount string) *model.Transaction {
	t.Helper()
	if kind == "" {
		kind = model.KindFlow
	}
	tx := &model.Transaction{
		UserID: userID, AccountID: accountID, Kind: kind,
		Amount:          decimal.RequireFromString(amount),
		TransactionDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Source: "manual",
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed txn: %v", err)
	}
	return tx
}

func TestTransferMatchingService_Confirm_Succeeds(t *testing.T) {
	svc, userID, accA, accB, g := newTransferFixture(t)
	ctx := context.Background()
	a := seedTransferTxn(t, g, userID, accA, "", "-100")
	b := seedTransferTxn(t, g, userID, accB, "", "100")

	linkedA, linkedB, err := svc.Confirm(ctx, userID, a.ID, b.ID)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !linkedA.IsTransfer || !linkedB.IsTransfer {
		t.Fatalf("expected both legs linked")
	}
}

func TestTransferMatchingService_Confirm_RejectsSameAccount(t *testing.T) {
	svc, userID, accA, _, g := newTransferFixture(t)
	ctx := context.Background()
	a := seedTransferTxn(t, g, userID, accA, "", "-50")
	b := seedTransferTxn(t, g, userID, accA, "", "50")

	if _, _, err := svc.Confirm(ctx, userID, a.ID, b.ID); !errors.Is(err, service.ErrTransferSameAccount) {
		t.Fatalf("Confirm same-account pair: got %v, want ErrTransferSameAccount", err)
	}
}

func TestTransferMatchingService_Confirm_RejectsNonFlowKind(t *testing.T) {
	svc, userID, accA, accB, g := newTransferFixture(t)
	ctx := context.Background()
	a := seedTransferTxn(t, g, userID, accA, model.KindAdjustment, "-50")
	b := seedTransferTxn(t, g, userID, accB, "", "50")

	if _, _, err := svc.Confirm(ctx, userID, a.ID, b.ID); !errors.Is(err, service.ErrTransferNotFlow) {
		t.Fatalf("Confirm with a non-flow leg: got %v, want ErrTransferNotFlow", err)
	}
}

func TestTransferMatchingService_Confirm_RejectsAmountMismatch(t *testing.T) {
	svc, userID, accA, accB, g := newTransferFixture(t)
	ctx := context.Background()
	a := seedTransferTxn(t, g, userID, accA, "", "-50")
	b := seedTransferTxn(t, g, userID, accB, "", "40")

	if _, _, err := svc.Confirm(ctx, userID, a.ID, b.ID); !errors.Is(err, service.ErrTransferAmountMismatch) {
		t.Fatalf("Confirm with mismatched amounts: got %v, want ErrTransferAmountMismatch", err)
	}
}

func TestTransferMatchingService_Confirm_RejectsAlreadyLinked(t *testing.T) {
	svc, userID, accA, accB, g := newTransferFixture(t)
	ctx := context.Background()
	a := seedTransferTxn(t, g, userID, accA, "", "-50")
	b := seedTransferTxn(t, g, userID, accB, "", "50")
	c := seedTransferTxn(t, g, userID, accB, "", "-50")

	if _, _, err := svc.Confirm(ctx, userID, a.ID, b.ID); err != nil {
		t.Fatalf("first Confirm: %v", err)
	}
	if _, _, err := svc.Confirm(ctx, userID, a.ID, c.ID); !errors.Is(err, service.ErrTransferAlreadyLinked) {
		t.Fatalf("Confirm an already-linked leg: got %v, want ErrTransferAlreadyLinked", err)
	}
}

func TestTransferMatchingService_Confirm_RejectsUnknownTransaction(t *testing.T) {
	svc, userID, accA, _, g := newTransferFixture(t)
	ctx := context.Background()
	a := seedTransferTxn(t, g, userID, accA, "", "-50")

	if _, _, err := svc.Confirm(ctx, userID, a.ID, 9_999_999_999); !errors.Is(err, service.ErrTransactionNotFound) {
		t.Fatalf("Confirm against a nonexistent id: got %v, want ErrTransactionNotFound", err)
	}
}

func TestTransferMatchingService_Unlink_RestoresBothLegs(t *testing.T) {
	svc, userID, accA, accB, g := newTransferFixture(t)
	ctx := context.Background()
	a := seedTransferTxn(t, g, userID, accA, "", "-80")
	b := seedTransferTxn(t, g, userID, accB, "", "80")
	if _, _, err := svc.Confirm(ctx, userID, a.ID, b.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	if err := svc.Unlink(ctx, userID, a.ID); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	txRepo := repository.NewTransactionRepository(g)
	reA, err := txRepo.GetByID(ctx, userID, a.ID)
	if err != nil {
		t.Fatalf("GetByID a: %v", err)
	}
	if reA.IsTransfer || reA.TransferPairID != nil {
		t.Fatalf("leg A still linked after Unlink: %+v", reA)
	}
	reB, err := txRepo.GetByID(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("GetByID b: %v", err)
	}
	if reB.IsTransfer || reB.TransferPairID != nil {
		t.Fatalf("leg B still linked after Unlink: %+v", reB)
	}
}

func TestTransferMatchingService_ProposeMatches_TenantIsolation(t *testing.T) {
	svc, userID, accA, accB, g := newTransferFixture(t)
	_, otherUserID, otherAccA, otherAccB, _ := newTransferFixture(t)
	ctx := context.Background()

	seedTransferTxn(t, g, userID, accA, "", "-60")
	seedTransferTxn(t, g, userID, accB, "", "60")
	seedTransferTxn(t, g, otherUserID, otherAccA, "", "-60")
	seedTransferTxn(t, g, otherUserID, otherAccB, "", "60")

	candidates, err := svc.ProposeMatches(ctx, userID)
	if err != nil {
		t.Fatalf("ProposeMatches: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1 (other user's matching pair must not leak)", len(candidates))
	}
	if candidates[0].A.UserID != userID || candidates[0].B.UserID != userID {
		t.Fatalf("candidate pair references a different user: %+v", candidates[0])
	}
}
