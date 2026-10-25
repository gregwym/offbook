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

// TestDashboard_MonthValidation covers the ?month= anchor on GET
// /dashboard/cash-flow (#609) and /dashboard/category-trend (#611): the
// branches that return before the repository runs. The anchored windows
// themselves are covered by the service DB tests.
func TestDashboard_MonthValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), 7))
		c.Next()
	})
	handler.NewDashboardHandler(service.NewDashboardService(nil, nil, nil, nil), nil).Register(api)

	nextMonth := time.Now().UTC().AddDate(0, 1, 0).Format("2006-01")
	tests := []struct {
		name, month string
	}{
		{"malformed", "2026-13"},
		{"full date", "2026-05-01"},
		{"not a month", "last-month"},
		{"future", nextMonth},
	}
	for _, route := range []string{"cash-flow", "category-trend"} {
		for _, tc := range tests {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/"+route+"?months=1&month="+tc.month, nil)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
				}
				if got := codeOf(t, w.Body.Bytes()); got != "INVALID_REQUEST" {
					t.Errorf("code = %q, want INVALID_REQUEST", got)
				}
			})
		}
	}
}
