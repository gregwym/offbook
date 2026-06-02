package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// These cover the handler-only branches of the AI import endpoints — the
// consent gate, the no-provider path, and job-id parsing — which short-circuit
// before any service/DB call. The extract/stage/commit logic itself is covered
// by service-layer DB tests.

type stubExtractorResolver struct {
	ex  ingestion.Extractor
	err error
}

func (s stubExtractorResolver) For(context.Context, int64) (ingestion.Extractor, error) {
	return s.ex, s.err
}

type noopExtractor struct{}

func (noopExtractor) Extract(context.Context, ingestion.Document) (*ingestion.Extraction, error) {
	return &ingestion.Extraction{Source: "pdf"}, nil
}
func (noopExtractor) Handles(string) bool { return true }
func (noopExtractor) Name() string        { return "noop" }

// importRouter builds a router whose TransactionHandler has nil repos (the
// service is never reached on these early-return paths) and the given resolver.
func importRouter(resolver handler.ExtractorResolver, userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := handler.NewTransactionHandler(service.NewTransactionService(nil, nil, nil))
	if resolver != nil {
		h.WithExtractorResolver(resolver)
	}
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), userID))
		c.Next()
	})
	h.Register(api)
	return r
}

func pdfMultipart(t *testing.T, consent bool) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "stmt.pdf")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write([]byte("%PDF-1.4 fake"))
	if consent {
		_ = w.WriteField("consent", "true")
	}
	_ = w.Close()
	return &body, w.FormDataContentType()
}

func codeOf(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &env)
	return env.Code
}

func TestImportExtract_RequiresConsent(t *testing.T) {
	r := importRouter(stubExtractorResolver{ex: noopExtractor{}}, 7)
	body, ct := pdfMultipart(t, false) // no consent
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/1/transactions/import/extract", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := codeOf(t, w.Body.Bytes()); got != "IMPORT_CONSENT_REQUIRED" {
		t.Errorf("code = %q, want IMPORT_CONSENT_REQUIRED", got)
	}
}

func TestImportExtract_NoProviderConfigured(t *testing.T) {
	// Resolver returns (nil, nil): consent given, but no extractor available.
	r := importRouter(stubExtractorResolver{ex: nil}, 7)
	body, ct := pdfMultipart(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/1/transactions/import/extract", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if got := codeOf(t, w.Body.Bytes()); got != "AI_IMPORT_UNAVAILABLE" {
		t.Errorf("code = %q, want AI_IMPORT_UNAVAILABLE", got)
	}
}

func TestImportExtract_NoResolverIsNotImplemented(t *testing.T) {
	r := importRouter(nil, 7) // no resolver wired at all
	body, ct := pdfMultipart(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/1/transactions/import/extract", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", w.Code)
	}
	if got := codeOf(t, w.Body.Bytes()); got != "AI_IMPORT_UNAVAILABLE" {
		t.Errorf("code = %q, want AI_IMPORT_UNAVAILABLE", got)
	}
}

func TestImportCommit_InvalidJobID(t *testing.T) {
	r := importRouter(stubExtractorResolver{ex: noopExtractor{}}, 7)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/import/jobs/not-a-number/commit", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := codeOf(t, w.Body.Bytes()); got != "INVALID_REQUEST" {
		t.Errorf("code = %q, want INVALID_REQUEST", got)
	}
}
