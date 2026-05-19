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

// DefaultClaudeModel is the model used when Request.Model is empty.
// Bumped per model release; tests pin to specific models when needed.
const DefaultClaudeModel = "claude-sonnet-4-6"

const (
	defaultClaudeEndpoint = "https://api.anthropic.com/v1/messages"
	anthropicVersion      = "2023-06-01"
	defaultMaxTokens      = 1024
)

// ClaudeProvider speaks the Anthropic Messages API directly over net/http —
// no SDK dependency. The streaming protocol is parsed inline (see
// readStream) so the package stays small and auditable per ADR-0003.
type ClaudeProvider struct {
	apiKey   string
	endpoint string // overridable for httptest fixtures
	http     *http.Client
}

// ClaudeConfig configures the provider. APIKey is required; Endpoint and
// HTTPClient have sensible defaults and are only set in tests.
type ClaudeConfig struct {
	APIKey     string
	Endpoint   string // defaults to defaultClaudeEndpoint
	HTTPClient *http.Client
}

// NewClaudeProvider validates config and returns a ready provider. Caller
// is responsible for not constructing one when CLAUDE_API_KEY is unset —
// the service layer hides the provider behind a settings check.
func NewClaudeProvider(cfg ClaudeConfig) (*ClaudeProvider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("ai: claude provider requires APIKey")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultClaudeEndpoint
	}
	client := cfg.HTTPClient
	if client == nil {
		// No timeout — streaming responses run as long as the model wants.
		// Cancellation is via ctx, which the request honors.
		client = &http.Client{}
	}
	return &ClaudeProvider{
		apiKey:   cfg.APIKey,
		endpoint: endpoint,
		http:     client,
	}, nil
}

// Name is the stable identifier persisted in ai_messages.provider.
func (p *ClaudeProvider) Name() string { return "claude" }

// Stream POSTs to /v1/messages with stream=true and surfaces text_delta
// events through a Delta channel. The returned channel is closed exactly
// once, after either a terminal Done delta or an Err delta.
func (p *ClaudeProvider) Stream(ctx context.Context, req Request) (<-chan Delta, error) {
	if len(req.Messages) == 0 {
		return nil, ErrEmptyRequest
	}

	model := req.Model
	if model == "" {
		model = DefaultClaudeModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	body := claudeRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Stream:    true,
		System:    req.System,
		Messages:  make([]claudeMessage, 0, len(req.Messages)),
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, claudeMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal claude request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("ai: build claude request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: claude request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Drain and close so the connection can be reused, then surface
		// the API error body to the caller. 401/403 → ErrUnauthorized so
		// the settings UI can prompt for a new key.
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(errBody)))
		}
		return nil, fmt.Errorf("ai: claude returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	out := make(chan Delta, 16)
	go p.readStream(ctx, resp.Body, out)
	return out, nil
}

// readStream parses the Anthropic SSE event stream and pushes Deltas. Owns
// resp.Body — closes on every exit path. Closes `out` exactly once after
// the terminal delta is sent.
func (p *ClaudeProvider) readStream(ctx context.Context, body io.ReadCloser, out chan<- Delta) {
	defer body.Close()
	defer close(out)

	scanner := bufio.NewScanner(body)
	// SSE lines can carry whole JSON payloads; default 64KiB buffer is
	// tight for large content_block_delta events.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		currentEvent string
		usage        Usage
		finishReason string
		sentDone     bool
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
		// Honor ctx between lines; the http stack will also abort the
		// underlying read, but a quick check here surfaces cancellation
		// without waiting for the next network read.
		if ctx.Err() != nil {
			return
		}

		line := scanner.Text()
		switch {
		case line == "":
			// blank line: event boundary. We dispatch on data: lines as
			// they arrive, so nothing to do here.
			continue
		case strings.HasPrefix(line, ":"):
			// SSE comment / heartbeat
			continue
		case strings.HasPrefix(line, "event:"):
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			switch currentEvent {
			case "content_block_delta":
				var ev struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					_ = send(Delta{Err: fmt.Errorf("ai: parse content_block_delta: %w", err)})
					return
				}
				if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
					if !send(Delta{Text: ev.Delta.Text}) {
						return
					}
				}
			case "message_delta":
				var ev struct {
					Delta struct {
						StopReason string `json:"stop_reason"`
					} `json:"delta"`
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					_ = send(Delta{Err: fmt.Errorf("ai: parse message_delta: %w", err)})
					return
				}
				if ev.Delta.StopReason != "" {
					finishReason = ev.Delta.StopReason
				}
				// message_delta usage carries only output_tokens. Input
				// tokens come from message_start.
				if ev.Usage.OutputTokens > 0 {
					usage.OutputTokens = ev.Usage.OutputTokens
				}
			case "message_start":
				var ev struct {
					Message struct {
						Usage struct {
							InputTokens  int `json:"input_tokens"`
							OutputTokens int `json:"output_tokens"`
						} `json:"usage"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err == nil {
					usage.InputTokens = ev.Message.Usage.InputTokens
					if ev.Message.Usage.OutputTokens > 0 {
						usage.OutputTokens = ev.Message.Usage.OutputTokens
					}
				}
			case "message_stop":
				_ = send(Delta{Done: true, FinishReason: finishReason, Usage: usage})
				sentDone = true
				return
			case "error":
				// Anthropic streams API-side errors as {"type":"error","error":{"type","message"}}
				var ev struct {
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
				}
				_ = json.Unmarshal([]byte(data), &ev)
				_ = send(Delta{Err: fmt.Errorf("ai: claude stream error: %s: %s", ev.Error.Type, ev.Error.Message)})
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		// ctx.Done propagates as a body-read error; classify it as cancellation.
		if ctx.Err() != nil {
			return
		}
		_ = send(Delta{Err: fmt.Errorf("ai: read claude stream: %w", err)})
		return
	}

	if !sentDone {
		// Stream ended without message_stop. Surface as error so the
		// caller doesn't silently persist a half-message.
		_ = send(Delta{Err: errors.New("ai: claude stream ended without message_stop")})
	}
}

// claudeRequest mirrors the Anthropic Messages API request body.
type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Compile-time assertion that ClaudeProvider satisfies Provider.
var _ Provider = (*ClaudeProvider)(nil)
