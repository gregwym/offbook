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

// TestAccountUpdateBalance_RequestValidation covers the handler-only branches
// of POST /accounts/:id/balance (#504), which return before the service runs.
// The reconcile behavior itself is covered by the service DB tests.
func TestAccountUpdateBalance_RequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), 7))
		c.Next()
	})
	handler.NewAccountHandler(service.NewAccountService(nil, nil, nil, nil)).Register(api)

	tests := []struct {
		name, path, body string
	}{
		{"missing balance", "/api/v1/accounts/1/balance", `{"as_of":"2026-01-02"}`},
		{"non-numeric balance", "/api/v1/accounts/1/balance", `{"balance":"lots"}`},
		{"bad as_of", "/api/v1/accounts/1/balance", `{"balance":"1.00","as_of":"01/02/2026"}`},
		{"bad id", "/api/v1/accounts/x/balance", `{"balance":"1.00"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
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
