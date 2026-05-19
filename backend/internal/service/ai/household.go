package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// HouseholdAccess is the slice of the household service the AI layer
// needs in order to gate /h/ai routes. It deliberately exposes only
// membership + a household-context build — never raw transactions or
// PII, preserving the boundary documented in ARCHITECTURE.md.
//
// Method names mirror household.Aggregator / household.Service so the
// production wiring can pass the existing services without an adapter.
type HouseholdAccess interface {
	// ActiveMembership returns the user's active membership row in the
	// given household, or `nil, false` when the user is not active.
	// In-grace members are excluded (they should be blocked from live
	// surfaces, matching the aggregator's lifecycle filter).
	ActiveMembership(ctx context.Context, userID, householdID int64) (*model.HouseholdMember, bool, error)

	// BuildAIContext returns the anonymized household context JSON that
	// goes into the system prompt for a shared thread. Returns (nil, nil)
	// when the household has nothing to share yet (no shared accounts);
	// the service falls back to the persona-only system prompt.
	BuildAIContext(ctx context.Context, userID, householdID int64) (json.RawMessage, error)
}

// ErrNotHouseholdMember is what the handler maps to 403 when the
// requester isn't an active member of the household they're addressing.
// In-grace members get the same status as non-members so the lifecycle
// is enforced uniformly.
var ErrNotHouseholdMember = errors.New("ai: not an active household member")

// WithHouseholdAccess wires the optional household access. When unset,
// the /h/ai surface methods (Create/List/Send) all return ErrThreadNotFound
// so a misconfigured deployment looks like a missing thread, not a leak.
func (s *Service) WithHouseholdAccess(a HouseholdAccess) *Service {
	s.household = a
	return s
}

// CreateSharedThread starts a household-shared thread bound to the
// caller's household. Returns ErrNotHouseholdMember if the user isn't
// active in the household.
func (s *Service) CreateSharedThread(ctx context.Context, userID, householdID int64, title *string) (*model.AIThread, error) {
	if s.household == nil {
		return nil, ErrNotHouseholdMember
	}
	if _, ok, err := s.household.ActiveMembership(ctx, userID, householdID); err != nil {
		return nil, fmt.Errorf("ai: check membership: %w", err)
	} else if !ok {
		return nil, ErrNotHouseholdMember
	}
	t := &model.AIThread{
		UserID:              userID,
		HouseholdID:         &householdID,
		SharedWithHousehold: true,
		Title:               title,
	}
	if err := s.threads.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("ai: create shared thread: %w", err)
	}
	return t, nil
}

// ListSharedThreads returns every shared thread in the household. The
// caller must be an active member; in-grace and non-members get
// ErrNotHouseholdMember.
func (s *Service) ListSharedThreads(ctx context.Context, userID, householdID int64) ([]model.AIThread, error) {
	if s.household == nil {
		return nil, ErrNotHouseholdMember
	}
	if _, ok, err := s.household.ActiveMembership(ctx, userID, householdID); err != nil {
		return nil, fmt.Errorf("ai: check membership: %w", err)
	} else if !ok {
		return nil, ErrNotHouseholdMember
	}
	return s.threads.ListSharedByHousehold(ctx, householdID)
}

// GetSharedThread is the household-aware GetThread: returns the thread
// when the user owns it OR when it's shared and the user is an active
// member of the household. Used by ListMessages + SendMessage to do
// auth in one place.
func (s *Service) GetSharedThread(ctx context.Context, userID, householdID, threadID int64) (*model.AIThread, error) {
	if s.household == nil {
		return nil, ErrThreadNotFound
	}
	if _, ok, err := s.household.ActiveMembership(ctx, userID, householdID); err != nil {
		return nil, fmt.Errorf("ai: check membership: %w", err)
	} else if !ok {
		return nil, ErrNotHouseholdMember
	}
	t, err := s.threads.GetByIDForMember(ctx, userID, householdID, threadID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrThreadNotFound
		}
		return nil, err
	}
	return t, nil
}

// ListSharedMessages is the household-aware ListMessages.
func (s *Service) ListSharedMessages(ctx context.Context, userID, householdID, threadID int64) ([]model.AIMessage, error) {
	if _, err := s.GetSharedThread(ctx, userID, householdID, threadID); err != nil {
		return nil, err
	}
	return s.messages.ListByThread(ctx, threadID)
}

// SendSharedMessage streams a turn in a shared thread. The system prompt
// is built from the household context (not the personal context). User
// attribution lands on `ai_messages.user_id` so the UI can render whose
// turn each message is. Same SSE shape as the personal SendMessage.
func (s *Service) SendSharedMessage(ctx context.Context, userID, householdID, threadID int64, content string) (<-chan SSEEvent, error) {
	provider, err := s.providerFor(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ai: resolve provider: %w", err)
	}
	if provider == nil {
		return nil, ErrNoProvider
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrEmptyMessage
	}
	thread, err := s.GetSharedThread(ctx, userID, householdID, threadID)
	if err != nil {
		return nil, err
	}
	if !thread.SharedWithHousehold {
		// Defensive: the household surface is for shared threads only.
		// A user's personal thread isn't reachable via /h/ai endpoints.
		return nil, ErrThreadNotFound
	}

	prior, err := s.messages.ListByThread(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("ai: list messages: %w", err)
	}
	uid := userID
	userMsg := &model.AIMessage{
		ThreadID: threadID,
		UserID:   &uid,
		Role:     string(RoleUser),
		Content:  content,
	}
	if err := s.messages.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("ai: persist user message: %w", err)
	}

	var ctxSnapshotJSON json.RawMessage
	systemPrompt := SystemPromptPrefix
	if raw, bErr := s.household.BuildAIContext(ctx, userID, householdID); bErr == nil && len(raw) > 0 {
		ctxSnapshotJSON = raw
		systemPrompt = SystemPromptPrefix + "\n\nHousehold context (aggregated across opted-in accounts only):\n" + string(raw)
	}

	req := Request{
		System: systemPrompt,
		Messages: convertHistory(prior, Message{
			Role:    RoleUser,
			Content: content,
		}),
	}
	deltas, err := provider.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai: provider stream: %w", err)
	}

	out := make(chan SSEEvent, 16)
	go s.relayProvider(ctx, threadID, provider, ctxSnapshotJSON, deltas, out)
	return out, nil
}
