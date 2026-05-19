package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// SystemPromptPrefix is prepended to every assistant turn ahead of the
// serialized Context. Keeping the persona terse — providers see the
// anonymized financial data immediately and the user message right after.
const SystemPromptPrefix = `You are a privacy-first financial assistant. ` +
	`You only see anonymized aggregate data — never names, account numbers, ` +
	`or other PII. Answer concisely. When asked about specific accounts, ` +
	`reason about the aggregates rather than guessing identifiers.`

// Errors surfaced to the handler. The handler maps each to an HTTP code
// and machine-readable code string.
var (
	ErrThreadNotFound = errors.New("ai thread not found")
	ErrEmptyMessage   = errors.New("message content must not be empty")
	ErrNoProvider     = errors.New("no AI provider configured")
)

// SSEEventType is the event-line value in the wire-format SSE stream.
type SSEEventType string

const (
	// SSEDelta carries one chunk of assistant text.
	SSEDelta SSEEventType = "delta"
	// SSEDone is the terminal sentinel. Carries finish_reason + usage.
	SSEDone SSEEventType = "done"
	// SSEError is a terminal error event. The stream closes after it.
	SSEError SSEEventType = "error"
)

// SSEEvent is what the service streams to the handler. The handler is
// responsible for writing it out as `event: <type>\ndata: <json>\n\n`.
type SSEEvent struct {
	Type SSEEventType    `json:"-"`
	Data json.RawMessage `json:"-"`
}

// DeltaPayload is the body of an SSEDelta event.
type DeltaPayload struct {
	Text string `json:"text"`
}

// DonePayload is the body of an SSEDone event.
type DonePayload struct {
	FinishReason string `json:"finish_reason,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	MessageID    int64  `json:"message_id"`
}

// ErrorPayload is the body of an SSEError event.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// Service is the orchestration layer: persistence + context build +
// provider stream. Constructor signature is deliberately fixed — adding
// `pii_repo` here would be the regression the noimport_test guards
// against.
type Service struct {
	threads  repository.AIThreadRepository
	messages repository.AIMessageRepository
	builder  *ContextBuilder
	provider Provider
}

// NewService wires the AI orchestration layer. The provider may be nil at
// boot — callers that haven't configured CLAUDE_API_KEY pass nil and the
// service rejects SendMessage with ErrNoProvider so the settings UI can
// surface a useful error.
func NewService(
	threads repository.AIThreadRepository,
	messages repository.AIMessageRepository,
	builder *ContextBuilder,
	provider Provider,
) *Service {
	return &Service{
		threads:  threads,
		messages: messages,
		builder:  builder,
		provider: provider,
	}
}

// ProviderConfigured reports whether SendMessage will work. Handlers can
// surface a 503-ish error before opening an SSE stream that's going to
// immediately close.
func (s *Service) ProviderConfigured() bool {
	return s.provider != nil
}

// CreateThread starts a new conversation for the user. Title is optional
// and may be filled in later — we deliberately don't auto-summarize from
// the first message because that's a UI-affordance decision.
func (s *Service) CreateThread(ctx context.Context, userID int64, title *string) (*model.AIThread, error) {
	t := &model.AIThread{
		UserID: userID,
		Title:  title,
	}
	if err := s.threads.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("ai: create thread: %w", err)
	}
	return t, nil
}

// ListThreads returns the user's threads, newest activity first.
func (s *Service) ListThreads(ctx context.Context, userID int64) ([]model.AIThread, error) {
	return s.threads.ListByUser(ctx, userID)
}

// GetThread fetches a single thread, scoped to user. Returns
// ErrThreadNotFound when the thread is missing or owned by a different
// user — they look the same on the wire to prevent ID-enumeration.
func (s *Service) GetThread(ctx context.Context, userID, id int64) (*model.AIThread, error) {
	t, err := s.threads.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrThreadNotFound
		}
		return nil, err
	}
	return t, nil
}

// ListMessages returns the turn history for a thread, oldest first. The
// caller MUST have already authorization-checked the thread (use GetThread
// first); ListByThread takes only thread_id.
func (s *Service) ListMessages(ctx context.Context, userID, threadID int64) ([]model.AIMessage, error) {
	if _, err := s.GetThread(ctx, userID, threadID); err != nil {
		return nil, err
	}
	return s.messages.ListByThread(ctx, threadID)
}

// SendMessage is the streaming entry point. It:
//  1. Authorization-checks the thread.
//  2. Persists the user message immediately.
//  3. Builds anonymized context.
//  4. Streams the provider response, forwarding token deltas via SSEEvent.
//  5. Persists the assistant message + context_snapshot on completion.
//
// The returned channel is closed after a terminal SSEDone or SSEError.
// Callers must drain or cancel ctx — otherwise the provider goroutine
// leaks. The handler's gin.Context cancellation handles this naturally
// when the HTTP client disconnects.
func (s *Service) SendMessage(ctx context.Context, userID, threadID int64, content string) (<-chan SSEEvent, error) {
	if !s.ProviderConfigured() {
		return nil, ErrNoProvider
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrEmptyMessage
	}
	if _, err := s.GetThread(ctx, userID, threadID); err != nil {
		return nil, err
	}

	// 1. Pull existing turns so the provider sees the conversation, then
	//    persist the new user turn. Doing the read first means a write
	//    error doesn't leave a hanging user message with no history
	//    context.
	prior, err := s.messages.ListByThread(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("ai: list messages: %w", err)
	}
	userMsg := &model.AIMessage{
		ThreadID: threadID,
		Role:     string(RoleUser),
		Content:  content,
	}
	if err := s.messages.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("ai: persist user message: %w", err)
	}

	// 2. Build anonymized context. A nil builder is allowed for tests;
	//    production wiring always passes one. On builder error we still
	//    stream — the assistant just gets the user message without the
	//    financial snapshot. Logging would help here, but the M7 service
	//    layer doesn't have a logger plumbed yet.
	var ctxSnapshotJSON json.RawMessage
	var systemPrompt = SystemPromptPrefix
	if s.builder != nil {
		bctx, err := s.builder.Build(ctx, userID)
		if err == nil && bctx != nil {
			if raw, mErr := json.Marshal(bctx); mErr == nil {
				ctxSnapshotJSON = raw
				systemPrompt = SystemPromptPrefix + "\n\nFinancial context:\n" + string(raw)
			}
		}
	}

	// 3. Provider request: system + every prior turn + the just-persisted
	//    user message. Order matches the on-disk creation order.
	req := Request{
		System: systemPrompt,
		Messages: convertHistory(prior, Message{
			Role:    RoleUser,
			Content: content,
		}),
	}
	deltas, err := s.provider.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai: provider stream: %w", err)
	}

	out := make(chan SSEEvent, 16)
	go s.relayProvider(ctx, threadID, ctxSnapshotJSON, deltas, out)
	return out, nil
}

// relayProvider owns the deltas channel and closes out exactly once.
// Concatenates streamed text into the persisted assistant message body.
func (s *Service) relayProvider(
	ctx context.Context,
	threadID int64,
	contextSnapshot json.RawMessage,
	deltas <-chan Delta,
	out chan<- SSEEvent,
) {
	defer close(out)

	providerName := s.provider.Name()
	var assembled strings.Builder
	var finishReason string
	var usage Usage

	send := func(ev SSEEvent) bool {
		select {
		case <-ctx.Done():
			return false
		case out <- ev:
			return true
		}
	}

	for d := range deltas {
		switch {
		case d.Err != nil:
			payload, _ := json.Marshal(ErrorPayload{Message: d.Err.Error(), Code: "PROVIDER_STREAM"})
			_ = send(SSEEvent{Type: SSEError, Data: payload})
			return
		case d.Done:
			finishReason = d.FinishReason
			usage = d.Usage
		default:
			if d.Text == "" {
				continue
			}
			assembled.WriteString(d.Text)
			payload, _ := json.Marshal(DeltaPayload{Text: d.Text})
			if !send(SSEEvent{Type: SSEDelta, Data: payload}) {
				return
			}
		}
	}

	// 4. Persist assistant message + snapshot. Use a background context for
	//    the write — the caller's ctx may have been cancelled at the moment
	//    the provider finished its final delta, and we still want the
	//    message saved if the model finished the response.
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	asst := &model.AIMessage{
		ThreadID:        threadID,
		Role:            string(RoleAssistant),
		Content:         assembled.String(),
		ContextSnapshot: contextSnapshot,
		Provider:        strPtr(providerName),
	}
	if err := s.messages.Create(persistCtx, asst); err != nil {
		payload, _ := json.Marshal(ErrorPayload{
			Message: "failed to persist assistant message: " + err.Error(),
			Code:    "PERSIST_FAILED",
		})
		_ = send(SSEEvent{Type: SSEError, Data: payload})
		return
	}

	donePayload, _ := json.Marshal(DonePayload{
		FinishReason: finishReason,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		MessageID:    asst.ID,
	})
	_ = send(SSEEvent{Type: SSEDone, Data: donePayload})
}

// convertHistory turns persisted ai_messages rows into provider-format
// turns, appending one extra Message at the end (typically the brand-new
// user turn). Anything other than user/assistant roles is dropped — the
// provider rejects unknown roles.
func convertHistory(prior []model.AIMessage, extra ...Message) []Message {
	out := make([]Message, 0, len(prior)+len(extra))
	for _, m := range prior {
		switch Role(m.Role) {
		case RoleUser, RoleAssistant:
			out = append(out, Message{Role: Role(m.Role), Content: m.Content})
		}
	}
	out = append(out, extra...)
	return out
}

func strPtr(s string) *string { return &s }
