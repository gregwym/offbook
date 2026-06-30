package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DefaultOpenAIModel is used when Request.Model is empty. It's a sane
// "is this thing on" default for real OpenAI; users override per-thread,
// and proxies that front a Claude/Codex subscription expect their own
// model strings — so this is rarely the effective value.
const DefaultOpenAIModel = "gpt-4o-mini"

// DefaultOpenAIBaseURL is the public OpenAI API root. The "/chat/completions"
// suffix is appended at request time, so a local OpenAI-compatible proxy is
// configured by pointing BaseURL at its "/v1" root (e.g. http://proxy:8080/v1).
const DefaultOpenAIBaseURL = "https://api.openai.com/v1"

// OpenAIProvider speaks the OpenAI Chat Completions streaming protocol over
// net/http — no SDK dependency, matching the audit posture of the Claude and
// Ollama providers (ADR-0003). It targets any OpenAI-compatible endpoint:
// real OpenAI, or a local proxy fronting a Claude Max / ChatGPT subscription.
type OpenAIProvider struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// OpenAIConfig configures the provider. APIKey is optional — some local
// proxies authenticate by other means and accept any (or no) bearer token —
// so an empty key simply omits the Authorization header. BaseURL defaults to
// DefaultOpenAIBaseURL; HTTPClient is only set in tests.
type OpenAIConfig struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// NewOpenAIProvider returns a ready provider. It does NOT validate the key or
// ping the endpoint at construction — the service layer hides the provider
// behind a settings check, mirroring the other providers.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultOpenAIBaseURL
	}
	base = strings.TrimRight(base, "/")
	client := cfg.HTTPClient
	if client == nil {
		// No timeout — streaming responses run as long as the model wants;
		// cancellation is via ctx, which the request honors.
		client = &http.Client{}
	}
	return &OpenAIProvider{apiKey: cfg.APIKey, baseURL: base, http: client}
}

// Name is the stable identifier persisted in ai_messages.provider.
func (p *OpenAIProvider) Name() string { return "openai" }

// Stream POSTs to {baseURL}/chat/completions with stream=true and surfaces
// each choices[].delta.content chunk as a Delta. The system prompt is sent
// as a leading system-role message. The returned channel is closed exactly
// once, after a terminal Done delta or an Err delta.
func (p *OpenAIProvider) Stream(ctx context.Context, req Request) (<-chan Delta, error) {
	if len(req.Messages) == 0 {
		return nil, ErrEmptyRequest
	}

	model := req.Model
	if model == "" {
		model = DefaultOpenAIModel
	}

	// OpenAI takes the system prompt as a leading system-role message, not a
	// separate field. Stitch it in so callers use the same Request shape.
	msgs := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openAIMessage{Role: string(m.Role), Content: m.Content})
	}

	body := openAIRequest{
		Model:    model,
		Messages: msgs,
		Stream:   true,
		// include_usage asks for a final chunk carrying token counts; without
		// it, streamed responses report no usage. Compatible proxies that
		// don't honor it simply omit the usage chunk (we tolerate absence).
		StreamOptions: &openAIStreamOptions{IncludeUsage: true},
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("ai: build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: openai request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Drain and close so the connection can be reused, then surface the
		// error body. 401/403 → ErrUnauthorized so the settings UI can prompt
		// for a new key.
		defer func() { _ = resp.Body.Close() }()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(errBody)))
		}
		return nil, fmt.Errorf("ai: openai returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	out := make(chan Delta, 16)
	go p.readStream(ctx, resp.Body, out)
	return out, nil
}

// readStream parses the OpenAI SSE stream and pushes Deltas. Owns resp.Body
// (closed on every exit path) and closes `out` exactly once after the
// terminal delta. The stream is a sequence of `data: {json}` lines ending
// with `data: [DONE]`; finish_reason and the optional usage chunk are
// accumulated and reported on the terminal Done delta.
func (p *OpenAIProvider) readStream(ctx context.Context, body io.ReadCloser, out chan<- Delta) {
	defer func() { _ = body.Close() }()
	defer close(out)

	scanner := bufio.NewScanner(body)
	// A single SSE data line carries a whole JSON chunk; bump the cap beyond
	// the default 64KiB so large chunks don't error with bufio.ErrTooLong.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		usage        Usage
		finishReason string
	)

	send := func(d Delta) bool {
		select {
		case <-ctx.Done():
			return false
		case out <- d:
			return true
		}
	}

	for scanner.Scan() {
		// Honor ctx between lines; the http stack also aborts the underlying
		// read, but a quick check surfaces cancellation sooner.
		if ctx.Err() != nil {
			return
		}

		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, ":"):
			// SSE comment / heartbeat.
			continue
		case !strings.HasPrefix(line, "data:"):
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			_ = send(Delta{Done: true, FinishReason: finishReason, Usage: usage})
			return
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			_ = send(Delta{Err: fmt.Errorf("ai: parse openai chunk: %w", err)})
			return
		}
		// Some compatible endpoints stream API-side errors as a top-level
		// "error" object instead of an HTTP status. Surface as terminal Err.
		if chunk.Error != nil && chunk.Error.Message != "" {
			_ = send(Delta{Err: fmt.Errorf("ai: openai stream error: %s", chunk.Error.Message)})
			return
		}
		if len(chunk.Choices) > 0 {
			c := chunk.Choices[0]
			if c.Delta.Content != "" {
				if !send(Delta{Text: c.Delta.Content}) {
					return
				}
			}
			if c.FinishReason != "" {
				finishReason = c.FinishReason
			}
		}
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		// ctx.Done propagates as a body-read error; classify as cancellation.
		if ctx.Err() != nil {
			return
		}
		_ = send(Delta{Err: fmt.Errorf("ai: read openai stream: %w", err)})
		return
	}

	// Scanner ended without `data: [DONE]` — surface as error so the caller
	// doesn't silently persist a half-message. (The [DONE] branch returns
	// early before reaching here.)
	_ = send(Delta{Err: errors.New("ai: openai stream ended without [DONE]")})
}

// openAIRequest mirrors POST /chat/completions body (streaming subset).
type openAIRequest struct {
	Model         string               `json:"model"`
	Messages      []openAIMessage      `json:"messages"`
	Stream        bool                 `json:"stream"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// openAIStreamChunk is one `data:` line. Intermediate chunks carry a
// choices[].delta.content fragment; the optional usage chunk (from
// include_usage) carries token counts with an empty choices array.
type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Compile-time assertion that OpenAIProvider satisfies Provider.
var _ Provider = (*OpenAIProvider)(nil)
