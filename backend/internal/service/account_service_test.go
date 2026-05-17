package service_test

import (
	"context"
	"errors"
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
	"github.com/gregwym/offbook/backend/internal/service"
)

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

func newAccountSvc(t *testing.T) (*service.AccountService, *gorm.DB) {
	t.Helper()
	g := openTestDB(t)
	return service.NewAccountService(repository.NewAccountRepository(g)), g
}

// validInput returns a creator that produces fresh, valid create inputs so
// each subtest gets its own (Name varies by suffix so we don't collide across
// runs that share a DB).
func validInput(suffix string) service.CreateAccountInput {
	return service.CreateAccountInput{
		Name:            "AcctSvc " + suffix,
		InstitutionSlug: "fixture",
		AccountType:     "checking",
		Currency:        "USD",
		Balance:         decimal.NewFromInt(0),
	}
}

func TestAccountService_Create_Validation(t *testing.T) {
	svc, _ := newAccountSvc(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		mutate  func(*service.CreateAccountInput)
		wantErr error
	}{
		{
			name:    "valid input succeeds",
			mutate:  func(*service.CreateAccountInput) {},
			wantErr: nil,
		},
		{
			name:    "empty name rejected",
			mutate:  func(in *service.CreateAccountInput) { in.Name = "   " },
			wantErr: service.ErrEmptyName,
		},
		{
			name:    "empty institution rejected",
			mutate:  func(in *service.CreateAccountInput) { in.InstitutionSlug = "" },
			wantErr: service.ErrEmptyInstitution,
		},
		{
			name:    "bogus account_type rejected",
			mutate:  func(in *service.CreateAccountInput) { in.AccountType = "bogus" },
			wantErr: service.ErrInvalidType,
		},
		{
			name:    "currency too long rejected",
			mutate:  func(in *service.CreateAccountInput) { in.Currency = "USDD" },
			wantErr: service.ErrInvalidCurrency,
		},
		{
			name: "last_four non-numeric rejected",
			mutate: func(in *service.CreateAccountInput) {
				v := "12ab"
				in.LastFour = &v
			},
			wantErr: service.ErrInvalidLastFour,
		},
		{
			name: "last_four wrong length rejected",
			mutate: func(in *service.CreateAccountInput) {
				v := "12"
				in.LastFour = &v
			},
			wantErr: service.ErrInvalidLastFour,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(t.Name() + "-" + tcSuffix(i))
			tc.mutate(&in)
			got, err := svc.Create(ctx, in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("Create err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got == nil || got.ID == 0 {
				t.Fatalf("expected created account, got %+v", got)
			}
			t.Cleanup(func() { _ = svc.SoftDelete(ctx, got.ID) })
		})
	}
}

func TestAccountService_SoftDelete_Idempotent(t *testing.T) {
	svc, _ := newAccountSvc(t)
	ctx := context.Background()

	acc, err := svc.Create(ctx, validInput("delete-idem"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.SoftDelete(ctx, acc.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	// Second delete must report not-found rather than silently succeeding —
	// "idempotent" here means the second call's RESULT is predictable, not
	// that we silently swallow the missing row. A real client retrying after
	// a network blip would want to know the row is gone.
	err = svc.SoftDelete(ctx, acc.ID)
	if !errors.Is(err, service.ErrAccountNotFound) {
		t.Errorf("second delete err = %v, want ErrAccountNotFound", err)
	}

	// And the account is invisible to Get.
	if _, err := svc.Get(ctx, acc.ID); !errors.Is(err, service.ErrAccountNotFound) {
		t.Errorf("Get after delete err = %v, want ErrAccountNotFound", err)
	}
}

// TestAccountService_Update_DoesNotTouchPII is the PII isolation contract
// test for the account service: an Update on an account must leave its
// pii_store rows untouched, since the service deliberately does not depend
// on pii_repo. This is the architectural cornerstone of the privacy model.
func TestAccountService_Update_DoesNotTouchPII(t *testing.T) {
	svc, g := newAccountSvc(t)
	ctx := context.Background()

	acc, err := svc.Create(ctx, validInput("pii-isolation"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, acc.ID) })

	// Manually insert a PII row outside the service so we know exactly what's
	// in the table before/after the update. (Going through PIIService would
	// also work but obscures what we're testing.)
	pii := model.PIIRecord{
		EntityType: "account",
		EntityID:   acc.ID,
		FieldName:  "holder_name",
		Value:      "Jane Doe Test",
	}
	if err := g.WithContext(ctx).Create(&pii).Error; err != nil {
		t.Fatalf("seed pii: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("entity_type = ? AND entity_id = ?", "account", acc.ID).
			Delete(&model.PIIRecord{})
	})

	// Update everything we can about the account that is NOT PII.
	newName := "Renamed via update"
	newType := "savings"
	newCurr := "EUR"
	newBalance := decimal.NewFromFloat(123.456)
	inactive := false
	if _, err := svc.Update(ctx, acc.ID, service.UpdateAccountInput{
		Name:        &newName,
		AccountType: &newType,
		Currency:    &newCurr,
		Balance:     &newBalance,
		IsActive:    &inactive,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// PII row must be byte-identical to what we seeded.
	var after model.PIIRecord
	if err := g.WithContext(ctx).First(&after, pii.ID).Error; err != nil {
		t.Fatalf("re-fetch pii: %v", err)
	}
	if after.Value != "Jane Doe Test" || after.FieldName != "holder_name" {
		t.Errorf("PII mutated by account update: got %+v", after)
	}
}

func tcSuffix(i int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return string(letters[i%len(letters)]) + time.Now().Format("150405.000000")
}
