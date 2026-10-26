package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// activeMember is a member repo whose only behavior is "the requester is an
// active member of household 1" — enough to reach the query validation.
type activeMember struct {
	repository.HouseholdMemberRepository
}

func (activeMember) GetMembershipForUser(context.Context, int64) (*model.HouseholdMember, error) {
	return &model.HouseholdMember{HouseholdID: 1}, nil
}

// TestHouseholdInsights_MonthValidation covers the household ?month= anchor
// (#619) on GET /h/insights/cash-flow and /h/insights/category-trend: the
// same 400 INVALID_REQUEST as personal for a malformed or future month,
// returned before the aggregator runs. The anchored windows themselves are
// covered by the aggregator DB tests.
func TestHouseholdInsights_MonthValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), 7))
		c.Next()
	})
	handler.NewHouseholdAggregatorHandler(nil, activeMember{}).Register(api)

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
				req := httptest.NewRequest(http.MethodGet, "/api/v1/h/insights/"+route+"?months=1&month="+tc.month, nil)
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
	t.Run("spend-by-category/malformed from", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/h/insights/spend-by-category?from=2026-04&to=2026-05-01", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
		}
	})
}
