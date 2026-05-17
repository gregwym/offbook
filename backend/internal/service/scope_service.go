package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// ErrInvalidScope is returned when a caller asks to switch to a scope that
// isn't one of {personal, household}, or that isn't available to them
// (e.g. asking for household when not a member).
var ErrInvalidScope = errors.New("invalid scope")

// ScopeView is the GET /me/scope payload. `active` is what the sidebar
// renders; `available` is the union the picker can switch to.
type ScopeView struct {
	Active      string   `json:"active"`
	Available   []string `json:"available"`
	HouseholdID *int64   `json:"household_id,omitempty"`
}

// ScopeService owns persistence of users.last_scope and computes the user's
// available scopes by checking active household membership. Touches users +
// household_members repos only.
type ScopeService struct {
	users   repository.UserRepository
	members repository.HouseholdMemberRepository
}

func NewScopeService(users repository.UserRepository, members repository.HouseholdMemberRepository) *ScopeService {
	return &ScopeService{users: users, members: members}
}

// Get reports the user's active + available scopes. If users.last_scope is
// household but the user is no longer in a household (e.g. left during grace
// without rejoining), we degrade to personal in the response without
// persisting — the next PATCH will pin it.
func (s *ScopeService) Get(ctx context.Context, userID int64) (*ScopeView, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	mem, err := s.members.GetMembershipForUser(ctx, userID)
	var hasHousehold bool
	var hhID *int64
	if err == nil && mem != nil && mem.LeftAt == nil {
		hasHousehold = true
		id := mem.HouseholdID
		hhID = &id
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("lookup membership: %w", err)
	}

	available := []string{model.ScopePersonal}
	if hasHousehold {
		available = append(available, model.ScopeHousehold)
	}

	active := u.LastScope
	if active == model.ScopeHousehold && !hasHousehold {
		active = model.ScopePersonal
	}
	if active != model.ScopePersonal && active != model.ScopeHousehold {
		active = model.ScopePersonal
	}
	return &ScopeView{Active: active, Available: available, HouseholdID: hhID}, nil
}

// Set persists users.last_scope. Rejects requests to enter household scope
// when the user is not an active member.
func (s *ScopeService) Set(ctx context.Context, userID int64, scope string) (*ScopeView, error) {
	switch scope {
	case model.ScopePersonal, model.ScopeHousehold:
		// ok
	default:
		return nil, ErrInvalidScope
	}
	if scope == model.ScopeHousehold {
		mem, err := s.members.GetMembershipForUser(ctx, userID)
		if err != nil || mem == nil || mem.LeftAt != nil {
			return nil, ErrInvalidScope
		}
	}
	if err := s.users.UpdateLastScope(ctx, userID, scope); err != nil {
		return nil, fmt.Errorf("update last_scope: %w", err)
	}
	return s.Get(ctx, userID)
}
