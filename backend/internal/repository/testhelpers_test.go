package repository_test

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// seedTestUser creates a throwaway user and registers cleanup. Used by
// integration tests that need a valid user_id for FK satisfaction.
func seedTestUser(t *testing.T, g *gorm.DB) int64 {
	t.Helper()
	u := &model.User{
		Email:                  fmt.Sprintf("repo-test-%d-%d@example.test", time.Now().UnixNano(), len(t.Name())),
		PasswordHash:           "x",
		LastScope:              model.ScopePersonal,
		DefaultScope:           model.ScopePersonal,
		PrimaryCurrencyAssetID: lookupUSDAssetID(t, g),
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	return u.ID
}

// lookupUSDAssetID returns the seeded USD fiat asset's id. Migration 13
// guarantees this row exists in any test DB.
func lookupUSDAssetID(t *testing.T, g *gorm.DB) int64 {
	t.Helper()
	var id int64
	if err := g.Raw(`SELECT id FROM assets WHERE symbol = 'USD' AND kind = 'fiat'`).Scan(&id).Error; err != nil {
		t.Fatalf("lookup USD asset: %v", err)
	}
	if id == 0 {
		t.Fatal("USD asset not seeded — migration 13 may not have run")
	}
	return id
}
