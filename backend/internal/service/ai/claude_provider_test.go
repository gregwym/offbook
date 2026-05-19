package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestClaudeProvider_Stream_FixtureRoundtrip pipes a recorded SSE response
// (testdata/claude_stream.sse) through the provider and checks the parsed
// Delta sequence. Locks down: text concatenation order, stop reason, usage
// rollup from message_start + message_delta, terminal Done sentinel, and
// channel close.
func TestClaudeProvider_Stream_FixtureRoundtrip(t *testing.T) {
	fixture, err := os.ReadFile("testdata/claude_stream.sse")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var gotReq claudeRequest
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
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	p, err := NewClaudeProvider(ClaudeConfig{
		APIKey:   "sk-test",
		Endpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClaudeProvider: %v", err)
	}

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
		texts    []string
		done     Delta
		gotDone  bool
		streamed []Delta
	)
	for d := range ch {
		streamed = append(streamed, d)
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
	if got, want := done.FinishReason, "end_turn"; got != want {
		t.Errorf("FinishReason = %q, want %q", got, want)
	}
	if got, want := done.Usage.InputTokens, 17; got != want {
		t.Errorf("InputTokens = %d, want %d", got, want)
	}
	if got, want := done.Usage.OutputTokens, 9; got != want {
		t.Errorf("OutputTokens = %d, want %d", got, want)
	}
	if last := streamed[len(streamed)-1]; !last.Done {
		t.Errorf("last delta should be Done, got %+v", last)
	}

	// Request payload sanity: defaults applied, stream=true, system + messages forwarded.
	if !gotReq.Stream {
		t.Errorf("request stream flag not set")
	}
	if gotReq.Model != DefaultClaudeModel {
		t.Errorf("model = %q, want default %q", gotReq.Model, DefaultClaudeModel)
	}
	if gotReq.MaxTokens != defaultMaxTokens {
		t.Errorf("max_tokens = %d, want default %d", gotReq.MaxTokens, defaultMaxTokens)
	}
	if gotReq.System != "You are a finance assistant." {
		t.Errorf("system not forwarded, got %q", gotReq.System)
	}
	if len(gotReq.Messages) != 1 || gotReq.Messages[0].Role != "user" || gotReq.Messages[0].Content != "hi" {
		t.Errorf("messages not forwarded correctly: %+v", gotReq.Messages)
	}
}

// TestClaudeProvider_Stream_Unauthorized maps 401 from the API to
// ErrUnauthorized so the settings UI can prompt for a new key without
// scraping error strings.
func TestClaudeProvider_Stream_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()

	p, _ := NewClaudeProvider(ClaudeConfig{APIKey: "sk-bad", Endpoint: srv.URL})
	_, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

// TestClaudeProvider_Stream_EmptyRequest short-circuits before any network IO.
func TestClaudeProvider_Stream_EmptyRequest(t *testing.T) {
	p, _ := NewClaudeProvider(ClaudeConfig{APIKey: "sk-test"})
	_, err := p.Stream(context.Background(), Request{})
	if !errors.Is(err, ErrEmptyRequest) {
		t.Fatalf("err = %v, want ErrEmptyRequest", err)
	}
}

// TestClaudeProvider_Stream_APIErrorEvent surfaces an `error` SSE event as
// a terminal Err delta instead of silently truncating the message.
func TestClaudeProvider_Stream_APIErrorEvent(t *testing.T) {
	body := "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"msg_x","usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n" +
		"event: error\n" +
		`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p, _ := NewClaudeProvider(ClaudeConfig{APIKey: "sk-test", Endpoint: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sawErr bool
	for d := range ch {
		if d.Err != nil {
			sawErr = true
			if !strings.Contains(d.Err.Error(), "Overloaded") {
				t.Errorf("err = %v, want it to mention Overloaded", d.Err)
			}
		}
	}
	if !sawErr {
		t.Fatal("expected Err delta from error SSE event")
	}
}

// TestClaudeProvider_Name pins the provenance string. Changing it would
// orphan existing ai_messages.provider rows.
func TestClaudeProvider_Name(t *testing.T) {
	p, _ := NewClaudeProvider(ClaudeConfig{APIKey: "sk-test"})
	if got, want := p.Name(), "claude"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}
