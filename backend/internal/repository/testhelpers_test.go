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
		Email:        fmt.Sprintf("repo-test-%d-%d@example.test", time.Now().UnixNano(), len(t.Name())),
		PasswordHash: "x",
		LastScope:    model.ScopePersonal,
		DefaultScope: model.ScopePersonal,
	}
	if err := g.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.User{}, u.ID)
	})
	return u.ID
}
