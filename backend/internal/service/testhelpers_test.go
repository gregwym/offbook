package service_test

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// seedTestUser creates a throwaway user and registers cleanup. The
// cleanup cascades by user_id over every table that carries it — keeps
// integration tests independent regardless of whether the test itself
// remembered to scrub its rows.
func seedTestUser(t *testing.T, g *gorm.DB) int64 {
	t.Helper()
	u := &model.User{
		Email:                  fmt.Sprintf("svc-test-%d-%d@example.test", time.Now().UnixNano(), len(t.Name())),
		PasswordHash:           "x",
		LastScope:              model.ScopePersonal,
		DefaultScope:           model.ScopePersonal,
		PrimaryCurrencyAssetID: testutil.LookupUSDAssetID(t, g),
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		// Cascade scrub: child rows first, then the user. None of these
		// tables have ON DELETE CASCADE, so order matters.
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Transaction{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Position{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Budget{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.SavingsGoal{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.CategorizationRule{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Account{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.AIThread{})
		g.Unscoped().Where("user_id = ?", u.ID).Delete(&model.Session{})
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	return u.ID
}
