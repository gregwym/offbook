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

// newTxSvc builds a real TransactionService backed by Postgres, plus a
// helper account + category for tests to attach transactions to. The helper
// rows are torn down at end-of-test via t.Cleanup.
func newTxSvc(t *testing.T) (svc *service.TransactionService, userID, accountID, categoryID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	accRepo := repository.NewAccountRepository(g)
	txRepo := repository.NewTransactionRepository(g)
	catRepo := repository.NewCategoryRepository(g)
	svc = service.NewTransactionService(txRepo, accRepo, catRepo)

	ctx := context.Background()
	userID = seedTestUser(t, g)
	acc := &model.Account{
		UserID:          userID,
		Name:            "tx-svc-fixture-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture",
		AccountType:     "checking",
		Currency:        "USD",
	}
	if err := g.WithContext(ctx).Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, acc.ID) })

	cat := &model.Category{
		Name:     "TxSvcFixture",
		Slug:     "tx-svc-fixture-" + time.Now().Format("150405.000000"),
		IsSystem: false,
	}
	if err := g.WithContext(ctx).Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	return svc, userID, acc.ID, cat.ID, g
}

func validTxInput(accountID int64) service.CreateTransactionInput {
	return service.CreateTransactionInput{
		AccountID:       accountID,
		Amount:          decimal.NewFromFloat(-12.34),
		Currency:        "USD",
		TransactionDate: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	}
}

func TestTransactionService_Create_Validation(t *testing.T) {
	svc, userID, accountID, categoryID, _ := newTxSvc(t)
	ctx := context.Background()
	created := []int64{}
	t.Cleanup(func() {
		for _, id := range created {
			_ = svc.SoftDelete(ctx, userID, id)
		}
	})

	cases := []struct {
		name    string
		mutate  func(*service.CreateTransactionInput)
		wantErr error
	}{
		{
			name:    "valid input succeeds",
			mutate:  func(*service.CreateTransactionInput) {},
			wantErr: nil,
		},
		{
			name:    "valid with category succeeds and sets categorization_method=manual",
			mutate:  func(in *service.CreateTransactionInput) { in.CategoryID = &categoryID },
			wantErr: nil,
		},
		{
			name:    "zero amount rejected",
			mutate:  func(in *service.CreateTransactionInput) { in.Amount = decimal.NewFromInt(0) },
			wantErr: service.ErrInvalidAmount,
		},
		{
			name:    "missing transaction_date rejected",
			mutate:  func(in *service.CreateTransactionInput) { in.TransactionDate = time.Time{} },
			wantErr: service.ErrMissingDate,
		},
		{
			name:    "nonexistent account rejected",
			mutate:  func(in *service.CreateTransactionInput) { in.AccountID = 999999 },
			wantErr: service.ErrAccountNotFound,
		},
		{
			name: "nonexistent category rejected",
			mutate: func(in *service.CreateTransactionInput) {
				bad := int64(999999)
				in.CategoryID = &bad
			},
			wantErr: service.ErrInvalidCategory,
		},
		{
			name:    "bogus source rejected",
			mutate:  func(in *service.CreateTransactionInput) { in.Source = "bogus" },
			wantErr: service.ErrInvalidSource,
		},
		{
			name:    "empty source defaults to manual",
			mutate:  func(in *service.CreateTransactionInput) { in.Source = "" },
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validTxInput(accountID)
			tc.mutate(&in)
			got, err := svc.Create(ctx, userID, in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("Create err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			created = append(created, got.ID)
			if got.Source != "manual" {
				t.Errorf("Source = %q, want manual (empty must default)", got.Source)
			}
			if tc.name == "valid with category succeeds and sets categorization_method=manual" {
				if got.CategorizationMethod == nil || *got.CategorizationMethod != "manual" {
					t.Errorf("categorization_method = %v, want manual", got.CategorizationMethod)
				}
			}
		})
	}
}

func TestTransactionService_AmountPrecision_Preserved(t *testing.T) {
	svc, userID, accountID, _, _ := newTxSvc(t)
	ctx := context.Background()

	wei := decimal.RequireFromString("0.000000000000000001")
	in := validTxInput(accountID)
	in.Amount = wei

	created, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, created.ID) })

	got, err := svc.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Amount.Equal(wei) {
		t.Errorf("amount round-trip lost precision: got %s, want %s", got.Amount, wei)
	}
}

func TestTransactionService_SoftDelete_AndGetReturns404(t *testing.T) {
	svc, userID, accountID, _, _ := newTxSvc(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, userID, validTxInput(accountID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.SoftDelete(ctx, userID, created.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if _, err := svc.Get(ctx, userID, created.ID); !errors.Is(err, service.ErrTransactionNotFound) {
		t.Errorf("Get after delete err = %v, want ErrTransactionNotFound", err)
	}
	if err := svc.SoftDelete(ctx, userID, created.ID); !errors.Is(err, service.ErrTransactionNotFound) {
		t.Errorf("second delete err = %v, want ErrTransactionNotFound", err)
	}
}

func TestTransactionService_Update_ClearCategoryWinsOverSet(t *testing.T) {
	svc, userID, accountID, categoryID, _ := newTxSvc(t)
	ctx := context.Background()

	in := validTxInput(accountID)
	in.CategoryID = &categoryID
	created, err := svc.Create(ctx, userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, created.ID) })

	bogus := int64(999999)
	updated, err := svc.Update(ctx, userID, created.ID, service.UpdateTransactionInput{
		ClearCategory: true,
		CategoryID:    &bogus,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.CategoryID != nil {
		t.Errorf("CategoryID = %v, want nil (clear should win)", *updated.CategoryID)
	}
	if updated.CategorizationMethod != nil {
		t.Errorf("CategorizationMethod = %v, want nil", *updated.CategorizationMethod)
	}
}

func TestTransactionService_List_PassesThroughToRepo(t *testing.T) {
	svc, userID, accountID, _, _ := newTxSvc(t)
	ctx := context.Background()

	in1 := validTxInput(accountID)
	in1.TransactionDate = time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	tx1, err := svc.Create(ctx, userID, in1)
	if err != nil {
		t.Fatalf("create tx1: %v", err)
	}
	in2 := validTxInput(accountID)
	in2.TransactionDate = time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	tx2, err := svc.Create(ctx, userID, in2)
	if err != nil {
		t.Fatalf("create tx2: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.SoftDelete(ctx, userID, tx1.ID)
		_ = svc.SoftDelete(ctx, userID, tx2.ID)
	})

	got, total, err := svc.List(ctx, userID, repository.TransactionFilter{
		AccountID: int64Ptr(accountID),
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(got) >= 2 && (got[0].ID != tx2.ID || got[1].ID != tx1.ID) {
		t.Errorf("ordering wrong: got [%d, %d], want [%d, %d]", got[0].ID, got[1].ID, tx2.ID, tx1.ID)
	}

	if err := svc.SoftDelete(ctx, userID, tx2.ID); err != nil {
		t.Fatalf("delete tx2: %v", err)
	}
	_, total, err = svc.List(ctx, userID, repository.TransactionFilter{AccountID: int64Ptr(accountID)})
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if total != 1 {
		t.Errorf("total after delete = %d, want 1", total)
	}
}

func int64Ptr(v int64) *int64 { return &v }
