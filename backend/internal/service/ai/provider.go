// Package ai is the AI assistant layer. It owns the provider abstraction
// (Claude, Ollama, future LLMs), the anonymized context builder, and the
// service that wires them to ai_threads/ai_messages.
//
// Architectural rule (ADR-0003, ADR-0005, ARCHITECTURE.md → PII Isolation):
// nothing in this package may import the pii_repo. Providers see only the
// anonymized context that context_builder produces. The check is enforced
// by a test in noimport_test.go.
package ai

import (
	"context"
	"errors"
)

// Role is one of "user", "assistant", or "system". System messages should
// usually go in Request.System instead of the Messages slice — Anthropic's
// API rejects role=system in messages.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn in the conversation. Content is a plain string; the
// provider is responsible for encoding into provider-specific shapes (e.g.
// Claude's content blocks).
type Message struct {
	Role    Role
	Content string
}

// Request is the provider-agnostic input to Provider.Stream. Callers should
// leave Model empty to use the provider's default — Provider implementations
// fill in their own default if it is unset.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	MaxTokens int
}

// Delta is one event from the streaming response. Exactly one of Text, Done,
// or Err is meaningful per delta:
//   - Text: incremental token text. Concatenate across deltas to reconstruct
//     the assistant message.
//   - Done: terminal sentinel. FinishReason ("end_turn", "max_tokens", ...)
//     and Usage are populated. Channel is closed after this delta is sent.
//   - Err: transport / parse / API error. Terminal; channel closes after.
//
// FinishReason and Usage are only meaningful on the Done delta.
type Delta struct {
	Text         string
	Done         bool
	FinishReason string
	Usage        Usage
	Err          error
}

// Usage reports token counts for one Stream call. Output is the running
// count from the provider; Input is the prompt count.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Provider is the minimum streaming surface the AI service depends on.
// Implementations: claude_provider.go, ollama_provider.go.
//
// Stream returns a receive-only channel of deltas. The provider owns the
// channel lifecycle: it MUST close the channel after sending a terminal
// delta (Done=true or Err set), and MUST stop sending if ctx is cancelled.
// Callers MUST drain or cancel — leaking a Stream call leaks the goroutine
// reading from the upstream HTTP body.
type Provider interface {
	Stream(ctx context.Context, req Request) (<-chan Delta, error)

	// Name returns a short stable identifier ("claude", "ollama") used in
	// ai_messages.provider for provenance and in settings UI.
	Name() string
}

// ErrEmptyRequest is returned when Request has no messages. Providers
// short-circuit before opening a network connection.
var ErrEmptyRequest = errors.New("ai: request has no messages")

// ErrUnauthorized signals the provider rejected the API key. Surfaces to
// the user as a settings-page hint to re-enter credentials.
var ErrUnauthorized = errors.New("ai: provider rejected credentials")
