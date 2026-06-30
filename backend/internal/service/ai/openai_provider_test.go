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

// TestOpenAIProvider_Stream_FixtureRoundtrip pipes a recorded SSE response
// through the provider and checks the Delta sequence. Locks down: text
// concatenation order, system-prompt prepending, finish_reason, usage rollup
// from the include_usage chunk, terminal Done sentinel, channel close.
func TestOpenAIProvider_Stream_FixtureRoundtrip(t *testing.T) {
	fixture := readFixture(t, "testdata/openai_stream.sse")

	var (
		gotReq  openAIRequest
		gotAuth string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{APIKey: "sk-test", BaseURL: srv.URL})

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

	// Request payload sanity: default model, stream flag, include_usage,
	// system prepended, bearer header set.
	if !gotReq.Stream {
		t.Errorf("stream flag not set")
	}
	if gotReq.StreamOptions == nil || !gotReq.StreamOptions.IncludeUsage {
		t.Errorf("stream_options.include_usage not set")
	}
	if gotReq.Model != DefaultOpenAIModel {
		t.Errorf("model = %q, want default %q", gotReq.Model, DefaultOpenAIModel)
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
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-test")
	}
}

// TestOpenAIProvider_Stream_NoAPIKeyOmitsAuth covers the proxy case: an empty
// key sends no Authorization header rather than "Bearer ".
func TestOpenAIProvider_Stream_NoAPIKeyOmitsAuth(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch { //nolint:revive // drain
	}
	if hadAuth {
		t.Error("Authorization header sent despite empty API key")
	}
}

// TestOpenAIProvider_Stream_Unauthorized maps 401 to ErrUnauthorized so the
// settings UI can prompt for a new key.
func TestOpenAIProvider_Stream_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{APIKey: "bad", BaseURL: srv.URL})
	_, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), ErrUnauthorized.Error()) {
		t.Errorf("err = %v, want it to wrap ErrUnauthorized", err)
	}
}

// TestOpenAIProvider_Stream_MidStreamError surfaces a top-level error object
// (some compatible proxies stream errors this way) as a terminal Err delta.
func TestOpenAIProvider_Stream_MidStreamError(t *testing.T) {
	body := `data: {"error":{"message":"upstream rate limited"}}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sawErr bool
	for d := range ch {
		if d.Err != nil {
			sawErr = true
			if !strings.Contains(d.Err.Error(), "rate limited") {
				t.Errorf("err = %v, want it to mention rate limited", d.Err)
			}
		}
	}
	if !sawErr {
		t.Fatal("expected Err delta for stream-side error")
	}
}

// TestOpenAIProvider_Stream_TruncatedStream surfaces a stream that ends
// without [DONE] as a terminal Err, so a half-message isn't persisted.
func TestOpenAIProvider_Stream_TruncatedStream(t *testing.T) {
	body := `data: {"choices":[{"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var (
		sawText bool
		sawErr  bool
	)
	for d := range ch {
		if d.Text != "" {
			sawText = true
		}
		if d.Err != nil {
			sawErr = true
		}
	}
	if !sawText {
		t.Error("expected the partial text delta")
	}
	if !sawErr {
		t.Error("expected Err delta for truncated stream")
	}
}

// TestOpenAIProvider_Stream_EmptyRequest short-circuits before network IO.
func TestOpenAIProvider_Stream_EmptyRequest(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	_, err := p.Stream(context.Background(), Request{})
	if err != ErrEmptyRequest {
		t.Fatalf("err = %v, want ErrEmptyRequest", err)
	}
}

// TestOpenAIProvider_Name pins the provenance string.
func TestOpenAIProvider_Name(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	if got, want := p.Name(), "openai"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

// TestOpenAIProvider_DefaultBaseURL ensures empty config targets public OpenAI.
func TestOpenAIProvider_DefaultBaseURL(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	if p.baseURL != DefaultOpenAIBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, DefaultOpenAIBaseURL)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return b
}
