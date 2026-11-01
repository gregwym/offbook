package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestReconcilePosition_LinksCausedByObservation proves the #370 audit
// linkage: the reconciling transaction ReconcilePosition writes carries the
// ID of the observation that caused it.
func TestReconcilePosition_LinksCausedByObservation(t *testing.T) {
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	accountID, usdID := seedReconcileAccount(t, g, userID)
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	obsRepo := repository.NewBalanceObservationRepository(g)

	rec, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(500), time.Now().UTC(), "plaid")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if rec == nil || rec.CausedByObservationID == nil {
		t.Fatalf("reconciling row = %+v, want a non-nil CausedByObservationID", rec)
	}

	obs, err := obsRepo.ListByAccountAsset(ctx, userID, accountID, usdID)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(obs) != 1 || *rec.CausedByObservationID != obs[0].ID {
		t.Errorf("CausedByObservationID = %v, want %d", rec.CausedByObservationID, obs[0].ID)
	}
}

// TestReconciliationService_Report builds a three-checkpoint history —
// opening balance, a small in-threshold adjustment, and a large
// unexplained adjustment — and asserts the report's per-checkpoint fields
// and the account-level NeedsAttention rollup.
func TestReconciliationService_Report(t *testing.T) {
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	accountID, usdID := seedReconcileAccount(t, g, userID)
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	obsRepo := repository.NewBalanceObservationRepository(g)
	acctRepo := repository.NewAccountRepository(g)

	now := time.Now().UTC()
	// Opening balance: 1000.
	if _, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(1000), now.Add(-2*time.Hour), "plaid"); err != nil {
		t.Fatalf("opening reconcile: %v", err)
	}
	// Small adjustment: +5 on a base of 1000 = 0.5%, below the 1% default
	// threshold — not flagged.
	if _, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(1005), now.Add(-1*time.Hour), "plaid"); err != nil {
		t.Fatalf("small adjustment reconcile: %v", err)
	}
	// Large adjustment: +500 on a base of 1005 ≈ 49.75% — flagged.
	if _, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(1505), now, "plaid"); err != nil {
		t.Fatalf("large adjustment reconcile: %v", err)
	}

	svc := service.NewReconciliationService(txRepo, obsRepo, acctRepo, 0.01)
	report, err := svc.Report(ctx, userID, accountID)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if !report.NeedsAttention {
		t.Errorf("report.NeedsAttention = false, want true (large adjustment unflagged)")
	}
	if len(report.Checkpoints) != 3 {
		t.Fatalf("checkpoints = %d, want 3", len(report.Checkpoints))
	}

	findByDelta := func(kind string, want decimal.Decimal) *service.ReconciliationCheckpoint {
		for i, cp := range report.Checkpoints {
			if cp.Kind == kind && cp.Delta.Equal(want) {
				return &report.Checkpoints[i]
			}
		}
		return nil
	}

	opening := findByDelta(model.KindOpeningBalance, decimal.NewFromInt(1000))
	if opening == nil {
		t.Fatalf("missing opening_balance checkpoint, got %+v", report.Checkpoints)
	}
	if opening.NeedsAttention {
		t.Errorf("opening_balance checkpoint flagged, want never flagged")
	}
	small := findByDelta(model.KindAdjustment, decimal.NewFromInt(5))
	if small == nil {
		t.Fatalf("missing small adjustment checkpoint, got %+v", report.Checkpoints)
	}
	if small.NeedsAttention {
		t.Errorf("small (0.5%%) adjustment flagged, want not flagged")
	}
	large := findByDelta(model.KindAdjustment, decimal.NewFromInt(500))
	if large == nil {
		t.Fatalf("missing large adjustment checkpoint, got %+v", report.Checkpoints)
	}
	if !large.NeedsAttention {
		t.Errorf("large (~49.75%%) adjustment not flagged, want flagged")
	}
	if large.TransactionID == nil {
		t.Errorf("large adjustment checkpoint has no TransactionID")
	}
}

// TestReconciliationService_Acknowledge proves only kind=adjustment rows are
// acknowledgeable, and that acknowledging clears the NeedsAttention flag
// without altering the ledger amount.
func TestReconciliationService_Acknowledge(t *testing.T) {
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	accountID, usdID := seedReconcileAccount(t, g, userID)
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	obsRepo := repository.NewBalanceObservationRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	now := time.Now().UTC()

	opening, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(1000), now.Add(-time.Hour), "plaid")
	if err != nil {
		t.Fatalf("opening reconcile: %v", err)
	}
	adjustment, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
		decimal.NewFromInt(2000), now, "plaid")
	if err != nil {
		t.Fatalf("adjustment reconcile: %v", err)
	}

	svc := service.NewReconciliationService(txRepo, obsRepo, acctRepo, 0.01)

	// Opening balance is never acknowledgeable.
	if _, err := svc.Acknowledge(ctx, userID, opening.ID, nil); err != service.ErrTransactionNotFound {
		t.Errorf("acknowledge opening_balance err = %v, want ErrTransactionNotFound", err)
	}

	note := "confirmed with bank statement"
	acked, err := svc.Acknowledge(ctx, userID, adjustment.ID, &note)
	if err != nil {
		t.Fatalf("acknowledge adjustment: %v", err)
	}
	if acked.AcknowledgedAt == nil {
		t.Errorf("acked.AcknowledgedAt is nil")
	}
	if acked.AcknowledgedNote == nil || *acked.AcknowledgedNote != note {
		t.Errorf("acked.AcknowledgedNote = %v, want %q", acked.AcknowledgedNote, note)
	}
	if !acked.Amount.Equal(decimal.NewFromInt(1000)) {
		t.Errorf("acknowledging altered the ledger amount: got %s, want 1000", acked.Amount)
	}

	report, err := svc.Report(ctx, userID, accountID)
	if err != nil {
		t.Fatalf("report after acknowledge: %v", err)
	}
	if report.NeedsAttention {
		t.Errorf("report.NeedsAttention = true after acknowledging the only flagged checkpoint")
	}

	flagged, err := svc.AccountsNeedingAttention(ctx, userID)
	if err != nil {
		t.Fatalf("accounts needing attention: %v", err)
	}
	if flagged[accountID] {
		t.Errorf("account still flagged in AccountsNeedingAttention after acknowledging")
	}
}

// TestReconciliationService_AccountsNeedingAttention_MultiUser proves the
// bulk flag query is scoped per-user — a different user's unacknowledged
// adjustment never flags this user's account (tenancy contract, testing.md).
func TestReconciliationService_AccountsNeedingAttention_MultiUser(t *testing.T) {
	g := openTestDB(t)
	userA := seedTestUser(t, g)
	userB := seedTestUser(t, g)
	accountA, usdID := seedReconcileAccount(t, g, userA)
	accountB, _ := seedReconcileAccount(t, g, userB)
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	obsRepo := repository.NewBalanceObservationRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	now := time.Now().UTC()

	// User B gets a large unacknowledged adjustment; user A gets none.
	if _, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userB, accountB, usdID,
		decimal.NewFromInt(1000), now.Add(-time.Hour), "plaid"); err != nil {
		t.Fatalf("user B opening: %v", err)
	}
	if _, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userB, accountB, usdID,
		decimal.NewFromInt(5000), now, "plaid"); err != nil {
		t.Fatalf("user B adjustment: %v", err)
	}

	svc := service.NewReconciliationService(txRepo, obsRepo, acctRepo, 0.01)
	flaggedForA, err := svc.AccountsNeedingAttention(ctx, userA)
	if err != nil {
		t.Fatalf("accounts needing attention for A: %v", err)
	}
	if flaggedForA[accountA] || flaggedForA[accountB] {
		t.Errorf("user A's flagged set leaked another user's account: %+v", flaggedForA)
	}

	flaggedForB, err := svc.AccountsNeedingAttention(ctx, userB)
	if err != nil {
		t.Fatalf("accounts needing attention for B: %v", err)
	}
	if !flaggedForB[accountB] {
		t.Errorf("user B's account not flagged: %+v", flaggedForB)
	}
}

// TestAccountService_ListResponse_AttentionReason proves the #697 reason
// fields: one flagged adjustment in the account's currency surfaces its
// magnitude exactly; a second flagged adjustment switches to a count with no
// single amount; another user's account carries none of it.
func TestAccountService_ListResponse_AttentionReason(t *testing.T) {
	g := openTestDB(t)
	userID := seedTestUser(t, g)
	otherID := seedTestUser(t, g)
	accountID, usdID := seedReconcileAccount(t, g, userID)
	seedReconcileAccount(t, g, otherID)
	ctx := context.Background()
	txRepo := repository.NewTransactionRepository(g)
	obsRepo := repository.NewBalanceObservationRepository(g)
	acctRepo := repository.NewAccountRepository(g)
	recon := service.NewReconciliationService(txRepo, obsRepo, acctRepo, 0.01)
	svc := service.NewAccountService(g, acctRepo, repository.NewAssetRepository(g), repository.NewPositionRepository(g)).
		WithReconciliation(recon)

	find := func(t *testing.T, uid, id int64) *service.AccountResponse {
		t.Helper()
		resps, _, err := svc.ListResponse(ctx, uid, repository.AccountFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for i := range resps {
			if resps[i].ID == id {
				return &resps[i]
			}
		}
		t.Fatalf("account %d not in list", id)
		return nil
	}

	now := time.Now().UTC()
	reconcile := func(qty string, at time.Time) {
		t.Helper()
		if _, err := service.ReconcilePosition(ctx, txRepo, obsRepo, userID, accountID, usdID,
			decimal.RequireFromString(qty), at, "plaid"); err != nil {
			t.Fatalf("reconcile %s: %v", qty, err)
		}
	}
	reconcile("1000", now.Add(-3*time.Hour)) // opening balance — never flagged
	if r := find(t, userID, accountID); r.NeedsAttention || r.AttentionCount != 0 || r.AttentionAmount != nil {
		t.Fatalf("opening only: got needs=%v count=%d amount=%v, want nothing flagged", r.NeedsAttention, r.AttentionCount, r.AttentionAmount)
	}

	// -20.37 on 1000 = 2.037% ≥ 1% → flagged; reason names 20.37 exactly.
	reconcile("979.63", now.Add(-2*time.Hour))
	r := find(t, userID, accountID)
	if !r.NeedsAttention || r.AttentionCount != 1 || r.AttentionAmount == nil {
		t.Fatalf("one adjustment: got needs=%v count=%d amount=%v", r.NeedsAttention, r.AttentionCount, r.AttentionAmount)
	}
	if !r.AttentionAmount.Equal(decimal.RequireFromString("20.37")) {
		t.Errorf("AttentionAmount = %s, want 20.37 (magnitude, exact)", r.AttentionAmount)
	}

	// A second flagged adjustment: count, no single amount.
	reconcile("1500", now.Add(-1*time.Hour))
	r = find(t, userID, accountID)
	if !r.NeedsAttention || r.AttentionCount != 2 || r.AttentionAmount != nil {
		t.Errorf("two adjustments: got needs=%v count=%d amount=%v, want count=2 amount=nil", r.NeedsAttention, r.AttentionCount, r.AttentionAmount)
	}

	others, _, err := svc.ListResponse(ctx, otherID, repository.AccountFilter{})
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	for _, o := range others {
		if o.ID == accountID || o.NeedsAttention || o.AttentionCount != 0 || o.AttentionAmount != nil {
			t.Errorf("other user's list leaked attention state: %+v", o)
		}
	}
}
