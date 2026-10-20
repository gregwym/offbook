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

// TestTransferDismiss_RequestValidation covers the branches of
// POST /transfers/dismiss (#510) that return before the repository runs.
// Ownership (404) and the effect on spending are covered by the DB tests.
func TestTransferDismiss_RequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), 7))
		c.Next()
	})
	handler.NewTransferHandler(service.NewTransferMatchingService(nil)).Register(api)

	tests := []struct {
		name, body, wantCode string
	}{
		{"missing ids", `{}`, "INVALID_REQUEST"},
		{"one id", `{"transaction_id_a":3}`, "INVALID_REQUEST"},
		{"malformed", `{"transaction_id_a":"x"}`, "INVALID_REQUEST"},
		{"same id twice", `{"transaction_id_a":3,"transaction_id_b":3}`, "INVALID_TRANSFER_MATCH"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/dismiss", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
			}
			if got := codeOf(t, w.Body.Bytes()); got != tc.wantCode {
				t.Errorf("code = %q, want %q", got, tc.wantCode)
			}
		})
	}
}
