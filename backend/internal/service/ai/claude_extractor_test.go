package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestClaudeExtractor_PDFRoundtrip drives a PDF document through the extractor
// against a fake Messages endpoint. Locks down: the document content block
// (base64 + media_type), the text instruction block, auth headers, and that a
// JSON object response (wrapped in a markdown fence, to exercise the tolerant
// parser) is parsed into rows + doc_totals.
func TestClaudeExtractor_PDFRoundtrip(t *testing.T) {
	var gotReq claudeExtractRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("x-api-key"), "sk-test"; got != want {
			t.Errorf("x-api-key = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("anthropic-version"), anthropicVersion; got != want {
			t.Errorf("anthropic-version = %q, want %q", got, want)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		// The model's text: the JSON object wrapped in a markdown fence + prose,
		// to exercise the tolerant parser. Marshal it so the outer JSON is valid
		// regardless of the embedded newlines.
		inner := "Here you go:\n```json\n" +
			`{"rows":[{"date":"2026-05-15","amount":"-4.50","description":"Coffee","confidence":0.96},` +
			`{"date":"2026-05-16","amount":"2000.00","description":"Paycheck","confidence":0.42}],` +
			`"doc_totals":["1995.50"],"notes":"two rows"}` +
			"\n```"
		writeExtractText(t, w, inner)
	}))
	defer srv.Close()

	ex, err := NewClaudeExtractor(ClaudeConfig{APIKey: "sk-test", Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("NewClaudeExtractor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := ex.ExtractDocument(ctx, StatementDocument{
		Filename: "stmt.pdf",
		MIME:     "application/pdf",
		Data:     []byte("%PDF-1.4 fake"),
	})
	if err != nil {
		t.Fatalf("ExtractDocument: %v", err)
	}

	// Request shape: a document block (base64 PDF) + a text instruction block.
	if len(gotReq.Messages) != 1 || len(gotReq.Messages[0].Content) != 2 {
		t.Fatalf("request content blocks = %+v, want 1 message / 2 blocks", gotReq.Messages)
	}
	docBlock := gotReq.Messages[0].Content[0]
	if docBlock.Type != "document" || docBlock.Source == nil || docBlock.Source.MediaType != "application/pdf" {
		t.Errorf("doc block = %+v, want document/application/pdf", docBlock)
	}
	if docBlock.Source.Type != "base64" || docBlock.Source.Data == "" {
		t.Errorf("doc source = %+v, want base64 with data", docBlock.Source)
	}
	if gotReq.Messages[0].Content[1].Type != "text" {
		t.Errorf("second block type = %q, want text", gotReq.Messages[0].Content[1].Type)
	}

	// Parsed result.
	if len(out.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(out.Rows))
	}
	if out.Rows[0].Amount != "-4.50" || out.Rows[0].Confidence != 0.96 {
		t.Errorf("row0 = %+v", out.Rows[0])
	}
	if out.Rows[1].Confidence != 0.42 {
		t.Errorf("row1 confidence = %v, want 0.42", out.Rows[1].Confidence)
	}
	if len(out.DocTotals) != 1 || out.DocTotals[0] != "1995.50" {
		t.Errorf("doc_totals = %v, want [1995.50]", out.DocTotals)
	}
}

func TestClaudeExtractor_ImageBlock(t *testing.T) {
	var gotReq claudeExtractRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"{\"rows\":[{\"date\":\"2026-01-02\",\"amount\":\"-9.99\",\"description\":\"x\",\"confidence\":0.8}]}"}]}`)
	}))
	defer srv.Close()

	ex, _ := NewClaudeExtractor(ClaudeConfig{APIKey: "k", Endpoint: srv.URL})
	_, err := ex.ExtractDocument(context.Background(), StatementDocument{MIME: "image/png", Data: []byte("\x89PNG")})
	if err != nil {
		t.Fatalf("ExtractDocument: %v", err)
	}
	if b := gotReq.Messages[0].Content[0]; b.Type != "image" || b.Source.MediaType != "image/png" {
		t.Errorf("image block = %+v, want image/image-png", b)
	}
}

func TestClaudeExtractor_UnsupportedMediaType(t *testing.T) {
	ex, _ := NewClaudeExtractor(ClaudeConfig{APIKey: "k", Endpoint: "http://unused"})
	_, err := ex.ExtractDocument(context.Background(), StatementDocument{MIME: "application/zip", Data: []byte("PK")})
	if err == nil || !strings.Contains(err.Error(), "unsupported document media type") {
		t.Fatalf("err = %v, want ErrUnsupportedMediaType", err)
	}
}

func TestClaudeExtractor_EmptyRowsIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"{\"rows\":[]}"}]}`)
	}))
	defer srv.Close()
	ex, _ := NewClaudeExtractor(ClaudeConfig{APIKey: "k", Endpoint: srv.URL})
	_, err := ex.ExtractDocument(context.Background(), StatementDocument{MIME: "application/pdf", Data: []byte("%PDF")})
	if err != ErrEmptyExtraction {
		t.Fatalf("err = %v, want ErrEmptyExtraction", err)
	}
}

func TestParseExtractionJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		rows int
	}{
		{"bare", `{"rows":[{"date":"2026-01-01","amount":"-1","description":"a","confidence":1}]}`, 1},
		{"fenced", "```json\n{\"rows\":[{\"date\":\"2026-01-01\",\"amount\":\"-1\",\"description\":\"a\",\"confidence\":1}]}\n```", 1},
		{"prose-wrapped", "Sure!\n{\"rows\":[{\"date\":\"2026-01-01\",\"amount\":\"-1\",\"description\":\"a\",\"confidence\":1}]}\nDone.", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := parseExtractionJSON(c.in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(out.Rows) != c.rows {
				t.Errorf("rows = %d, want %d", len(out.Rows), c.rows)
			}
		})
	}
}

// writeExtractText writes a Messages API response whose single text block is
// `text`, marshaled so any embedded newlines/quotes stay valid JSON.
func writeExtractText(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	resp := claudeExtractResponse{StopReason: "end_turn"}
	resp.Content = append(resp.Content, struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: "text", Text: text})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestClaudeExtractor_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()
	ex, _ := NewClaudeExtractor(ClaudeConfig{APIKey: "k", Endpoint: srv.URL})
	_, err := ex.ExtractDocument(context.Background(), StatementDocument{MIME: "application/pdf", Data: []byte("%PDF")})
	if err == nil || !strings.Contains(err.Error(), "rejected credentials") {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}
