package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOllamaProvider_Stream_FixtureRoundtrip pipes a recorded NDJSON
// response through the provider and checks the Delta sequence. Locks down:
// text concatenation order, system-prompt prepending, done_reason +
// prompt/eval-count rollup, terminal Done sentinel, channel close.
func TestOllamaProvider_Stream_FixtureRoundtrip(t *testing.T) {
	fixture, err := os.ReadFile("testdata/ollama_stream.ndjson")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var gotReq ollamaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	p := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := p.Stream(ctx, Request{
		System:   "You are a finance assistant.",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var (
		texts   []string
		done    Delta
		gotDone bool
	)
	for d := range ch {
		switch {
		case d.Err != nil:
			t.Fatalf("unexpected error delta: %v", d.Err)
		case d.Done:
			done = d
			gotDone = true
		default:
			texts = append(texts, d.Text)
		}
	}

	if !gotDone {
		t.Fatal("stream ended without a Done delta")
	}
	if got, want := strings.Join(texts, ""), "Hello, world!"; got != want {
		t.Errorf("concatenated text = %q, want %q", got, want)
	}
	if got, want := done.FinishReason, "stop"; got != want {
		t.Errorf("FinishReason = %q, want %q", got, want)
	}
	if got, want := done.Usage.InputTokens, 17; got != want {
		t.Errorf("InputTokens = %d, want %d", got, want)
	}
	if got, want := done.Usage.OutputTokens, 9; got != want {
		t.Errorf("OutputTokens = %d, want %d", got, want)
	}

	// Request payload sanity: default model, stream flag, system prepended.
	if !gotReq.Stream {
		t.Errorf("stream flag not set")
	}
	if gotReq.Model != DefaultOllamaModel {
		t.Errorf("model = %q, want default %q", gotReq.Model, DefaultOllamaModel)
	}
	if len(gotReq.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2 (system + user)", len(gotReq.Messages))
	}
	if gotReq.Messages[0].Role != "system" || gotReq.Messages[0].Content != "You are a finance assistant." {
		t.Errorf("first message = %+v, want system prompt", gotReq.Messages[0])
	}
	if gotReq.Messages[1].Role != "user" || gotReq.Messages[1].Content != "hi" {
		t.Errorf("second message = %+v, want user 'hi'", gotReq.Messages[1])
	}
}

// TestOllamaProvider_Stream_DaemonError surfaces a mid-stream {"error": ...}
// line as a terminal Err delta.
func TestOllamaProvider_Stream_DaemonError(t *testing.T) {
	body := `{"error":"model 'bogus' not found, try pulling it first"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sawErr bool
	for d := range ch {
		if d.Err != nil {
			sawErr = true
			if !strings.Contains(d.Err.Error(), "not found") {
				t.Errorf("err = %v, want it to mention not found", d.Err)
			}
		}
	}
	if !sawErr {
		t.Fatal("expected Err delta for daemon-side error")
	}
}

// TestOllamaProvider_Stream_EmptyRequest short-circuits before network IO.
func TestOllamaProvider_Stream_EmptyRequest(t *testing.T) {
	p := NewOllamaProvider(OllamaConfig{})
	_, err := p.Stream(context.Background(), Request{})
	if err != ErrEmptyRequest {
		t.Fatalf("err = %v, want ErrEmptyRequest", err)
	}
}

// TestOllamaProvider_Name pins the provenance string.
func TestOllamaProvider_Name(t *testing.T) {
	p := NewOllamaProvider(OllamaConfig{})
	if got, want := p.Name(), "ollama"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

// TestOllamaProvider_DefaultBaseURL ensures empty config uses the
// conventional local bind.
func TestOllamaProvider_DefaultBaseURL(t *testing.T) {
	p := NewOllamaProvider(OllamaConfig{})
	if p.baseURL != DefaultOllamaBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, DefaultOllamaBaseURL)
	}
}
