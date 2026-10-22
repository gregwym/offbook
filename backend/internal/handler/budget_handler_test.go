package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// TestBudgetSpend_OnValidation covers the ?on= branches of
// GET /budgets/:id/spend (#577) that return before the repository runs.
// Window selection and ownership are covered by the service DB tests.
func TestBudgetSpend_OnValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), 7))
		c.Next()
	})
	handler.NewBudgetHandler(service.NewBudgetService(nil, nil)).Register(api)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	tests := []struct {
		name, on string
	}{
		{"malformed", "2026-13-01"},
		{"not a date", "last-month"},
		{"timestamp", "2026-05-01T00:00:00Z"},
		{"future", tomorrow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/budgets/3/spend?on="+tc.on, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
			}
			if got := codeOf(t, w.Body.Bytes()); got != "INVALID_DATE" {
				t.Errorf("code = %q, want INVALID_DATE", got)
			}
		})
	}
}
