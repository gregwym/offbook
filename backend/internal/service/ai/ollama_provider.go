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

// DefaultOllamaModel is the model used when Request.Model is empty. Picked
// for being a sane "is this thing on" default — users override per-thread
// from the Settings/model-switcher UI.
const DefaultOllamaModel = "llama3:8b"

// DefaultOllamaBaseURL is the conventional local Ollama bind address.
const DefaultOllamaBaseURL = "http://localhost:11434"

// OllamaProvider streams Ollama's /api/chat NDJSON responses through the
// shared Delta channel. Because Ollama runs locally, there's no API key —
// the only required config is the base URL.
type OllamaProvider struct {
	baseURL string
	http    *http.Client
}

// OllamaConfig configures the provider. BaseURL defaults to
// DefaultOllamaBaseURL when empty.
type OllamaConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewOllamaProvider returns a ready provider. It does NOT ping Ollama at
// construction — the service layer probes /api/tags on a settings-page
// "test connection" action so a missing daemon doesn't take the backend
// down at boot.
func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultOllamaBaseURL
	}
	base = strings.TrimRight(base, "/")
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &OllamaProvider{baseURL: base, http: client}
}

// Name is the stable identifier persisted in ai_messages.provider.
func (p *OllamaProvider) Name() string { return "ollama" }

// Stream POSTs to {baseURL}/api/chat with stream=true and surfaces each
// NDJSON line as a Delta. The provider owns the channel: it closes after
// the terminal Done delta (or an Err delta) and honors ctx cancellation.
func (p *OllamaProvider) Stream(ctx context.Context, req Request) (<-chan Delta, error) {
	if len(req.Messages) == 0 {
		return nil, ErrEmptyRequest
	}

	model := req.Model
	if model == "" {
		model = DefaultOllamaModel
	}

	// Ollama's /api/chat takes the system prompt as a first system-role
	// message, not a separate field. Stitch it in here so callers can use
	// the same Request shape across providers.
	msgs := make([]ollamaMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, ollamaMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, ollamaMessage{Role: string(m.Role), Content: m.Content})
	}

	body := ollamaRequest{
		Model:    model,
		Messages: msgs,
		Stream:   true,
	}
	// MaxTokens maps to Ollama's options.num_predict — left absent when 0
	// so Ollama uses its server-side default.
	if req.MaxTokens > 0 {
		body.Options = &ollamaOptions{NumPredict: req.MaxTokens}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("ai: build ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: ollama request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return nil, fmt.Errorf("ai: ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	out := make(chan Delta, 16)
	go p.readStream(ctx, resp.Body, out)
	return out, nil
}

// readStream owns resp.Body and the out channel: both are closed before
// return. NDJSON parsing is one JSON object per line; the terminal object
// carries done=true plus prompt/eval token counts.
func (p *OllamaProvider) readStream(ctx context.Context, body io.ReadCloser, out chan<- Delta) {
	defer func() { _ = body.Close() }()
	defer close(out)

	scanner := bufio.NewScanner(body)
	// A single Ollama "message" line can be long — bump the line cap so
	// large content chunks don't error with bufio.ErrTooLong.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	send := func(d Delta) bool {
		select {
		case <-ctx.Done():
			return false
		case out <- d:
			return true
		}
	}

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev ollamaStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			_ = send(Delta{Err: fmt.Errorf("ai: parse ollama line: %w", err)})
			return
		}
		// Ollama also reports daemon-side errors mid-stream as a top-level
		// "error" field. Surface as a terminal Err delta.
		if ev.Error != "" {
			_ = send(Delta{Err: fmt.Errorf("ai: ollama stream error: %s", ev.Error)})
			return
		}
		if ev.Message.Content != "" {
			if !send(Delta{Text: ev.Message.Content}) {
				return
			}
		}
		if ev.Done {
			_ = send(Delta{
				Done:         true,
				FinishReason: ev.DoneReason,
				Usage: Usage{
					InputTokens:  ev.PromptEvalCount,
					OutputTokens: ev.EvalCount,
				},
			})
			return
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return
		}
		_ = send(Delta{Err: fmt.Errorf("ai: read ollama stream: %w", err)})
		return
	}

	// Stream closed without a done=true line. Surface as error so the
	// caller doesn't silently persist a half-message.
	_ = send(Delta{Err: errors.New("ai: ollama stream ended without done=true")})
}

// ollamaRequest mirrors POST /api/chat body.
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	NumPredict int `json:"num_predict,omitempty"`
}

// ollamaStreamEvent is one NDJSON line. The done=true line carries
// done_reason + eval counts; intermediate lines just carry message.content.
type ollamaStreamEvent struct {
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
	Error           string        `json:"error"`
}

// Compile-time assertion that OllamaProvider satisfies Provider.
var _ Provider = (*OllamaProvider)(nil)
