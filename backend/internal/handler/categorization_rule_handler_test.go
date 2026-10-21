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

// TestCategorizationRuleApplyRuns_Routes covers the apply-run routes (#565)
// without a DB: they register next to GET /categorization-rules/:id, "no
// run yet" is {"data": null} rather than a 404, and an unknown run or a bad
// id maps to 404 / 400. Restore semantics are covered by the service DB tests.
func TestCategorizationRuleApplyRuns_Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), 7))
		c.Next()
	})
	handler.NewCategorizationRuleHandler(service.NewCategorizationRuleService(nil, nil)).Register(api)

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantCode, wantBody string
	}{
		{"latest with no run", http.MethodGet, "/api/v1/categorization-rules/apply-runs/latest", http.StatusOK, "", `{"data":null}`},
		{"undo unknown run", http.MethodPost, "/api/v1/categorization-rules/apply-runs/42/undo", http.StatusNotFound, "RULE_APPLY_RUN_NOT_FOUND", ""},
		{"undo bad id", http.MethodPost, "/api/v1/categorization-rules/apply-runs/x/undo", http.StatusBadRequest, "INVALID_REQUEST", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantCode != "" {
				if got := codeOf(t, w.Body.Bytes()); got != tc.wantCode {
					t.Errorf("code = %q, want %q", got, tc.wantCode)
				}
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Errorf("body = %s, want %s", w.Body.String(), tc.wantBody)
			}
		})
	}
}
