package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// aiHouseholdAccess adapts the existing household services to the
// `ai.HouseholdAccess` interface. Putting it in package router keeps the
// service/ai package free of any household-package dependency (matches
// the layering convention: services don't reach across to peer service
// packages — wiring code does).
type aiHouseholdAccess struct {
	members    repository.HouseholdMemberRepository
	aggregator *household.Aggregator
}

func newAIHouseholdAccess(
	members repository.HouseholdMemberRepository,
	aggregator *household.Aggregator,
) *aiHouseholdAccess {
	return &aiHouseholdAccess{members: members, aggregator: aggregator}
}

// ActiveMembership reports whether the user has an active (not left, not
// purged) row in the household. In-grace membership returns (nil, false).
func (a *aiHouseholdAccess) ActiveMembership(ctx context.Context, userID, householdID int64) (*model.HouseholdMember, bool, error) {
	m, err := a.members.GetActive(ctx, householdID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return m, true, nil
}

// BuildAIContext serializes the aggregator's HouseholdAIContext into the
// raw-JSON shape the AI service embeds in the system prompt. A nil return
// means "nothing to share yet" (no shared accounts / dissolved household);
// the AI service falls back to the persona-only prompt.
func (a *aiHouseholdAccess) BuildAIContext(ctx context.Context, userID, householdID int64) (json.RawMessage, error) {
	out, err := a.aggregator.AIContext(ctx, householdID, userID)
	if err != nil {
		return nil, fmt.Errorf("router: build household ai context: %w", err)
	}
	if out == nil {
		return nil, nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("router: marshal household ai context: %w", err)
	}
	return raw, nil
}
