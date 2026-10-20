package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// TestCategorizationRuleReorder_RequestValidation covers the branches of
// POST /categorization-rules/reorder (#540) that return before the service
// touches the DB. Order mismatches and tenant isolation are covered by the
// service DB tests.
func TestCategorizationRuleReorder_RequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), 7))
		c.Next()
	})
	handler.NewCategorizationRuleHandler(service.NewCategorizationRuleService(nil, nil)).Register(api)

	tests := []struct {
		name, body string
	}{
		{"missing rule_ids", `{}`},
		{"malformed ids", `{"rule_ids":["x"]}`},
		{"not json", `rule_ids=1`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/categorization-rules/reorder", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
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
