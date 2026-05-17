package household

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// InviteTTL is how long a freshly minted invite token remains acceptable.
const InviteTTL = 7 * 24 * time.Hour

// HouseholdDetail is the GET /households/:id payload — household + active members.
type HouseholdDetail struct {
	Household *model.Household         `json:"household"`
	Members   []model.HouseholdMember  `json:"members"`
	Role      string                   `json:"role"` // requester's role in this household
}

// CreateInput is the body for POST /households.
type CreateInput struct {
	Name            string
	GracePeriodDays *int
}

// UpdateInput patches name and/or grace_period_days. Owner-only.
type UpdateInput struct {
	Name            *string
	GracePeriodDays *int
}

// CreateInviteInput is the body for POST /households/:id/invites.
type CreateInviteInput struct {
	Role string // owner | contributor | view_only; defaults to contributor
}

// CreateInviteResult bundles the raw token (only thing the invitee needs)
// with the persisted invite metadata.
type CreateInviteResult struct {
	Invite *model.HouseholdInvite `json:"invite"`
	Token  string                 `json:"token"`
}

// AcceptResult is what POST /invites/:token/accept returns.
type AcceptResult struct {
	Household *model.Household       `json:"household"`
	Member    *model.HouseholdMember `json:"member"`
	Resumed   bool                   `json:"resumed"`
}

// Service owns household lifecycle + per-account share management. It is the
// SECOND non-aggregator service in service/household — the aggregator (next
// issue) handles cross-user reads of domain data and shares this package's
// import boundary: NEVER pii_repo.
type Service struct {
	households repository.HouseholdRepository
	members    repository.HouseholdMemberRepository
	invites    repository.HouseholdInviteRepository
	shares     repository.AccountShareRepository
	accounts   repository.AccountRepository
	config     repository.InstanceConfigRepository
	users      repository.UserRepository
	secret     string
	now        func() time.Time
}

func NewService(
	households repository.HouseholdRepository,
	members repository.HouseholdMemberRepository,
	invites repository.HouseholdInviteRepository,
	shares repository.AccountShareRepository,
	accounts repository.AccountRepository,
	config repository.InstanceConfigRepository,
	users repository.UserRepository,
	secret string,
) *Service {
	return &Service{
		households: households,
		members:    members,
		invites:    invites,
		shares:     shares,
		accounts:   accounts,
		config:     config,
		users:      users,
		secret:     secret,
		now:        time.Now,
	}
}

// SetClock lets tests freeze time for invite-expiry assertions.
func (s *Service) SetClock(fn func() time.Time) { s.now = fn }

// --- Household CRUD ---

// Create makes a new household with the caller as `owner`. A user can belong
// to at most one household (ADR-0006) — callers already in one get ErrAlreadyMember.
func (s *Service) Create(ctx context.Context, userID int64, in CreateInput) (*model.Household, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrEmptyName
	}
	grace := 30
	if in.GracePeriodDays != nil {
		if *in.GracePeriodDays < 0 {
			return nil, ErrInvalidGrace
		}
		grace = *in.GracePeriodDays
	}
	// At-most-one-household invariant.
	if existing, err := s.members.GetMembershipForUser(ctx, userID); err == nil && existing != nil {
		return nil, ErrAlreadyMember
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("check existing membership: %w", err)
	}

	h := &model.Household{
		Name:            name,
		OwnerID:         userID,
		GracePeriodDays: grace,
	}
	if err := s.households.Create(ctx, h); err != nil {
		return nil, fmt.Errorf("create household: %w", err)
	}
	m := &model.HouseholdMember{
		HouseholdID: h.ID,
		UserID:      userID,
		Role:        model.RoleOwner,
		JoinedAt:    s.now(),
	}
	if err := s.members.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("seed owner member: %w", err)
	}
	return h, nil
}

// Get returns the household detail (with members) provided the caller is an
// active member. Non-members get ErrNotMember (we don't leak existence).
func (s *Service) Get(ctx context.Context, userID, householdID int64) (*HouseholdDetail, error) {
	mem, err := s.members.GetActive(ctx, householdID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotMember
		}
		return nil, err
	}
	h, err := s.households.GetByID(ctx, householdID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrHouseholdNotFound
		}
		return nil, err
	}
	members, err := s.members.ListActive(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return &HouseholdDetail{Household: h, Members: members, Role: mem.Role}, nil
}

// Update mutates name / grace_period_days. Owner-only.
func (s *Service) Update(ctx context.Context, userID, householdID int64, in UpdateInput) (*model.Household, error) {
	if err := s.requireOwner(ctx, userID, householdID); err != nil {
		return nil, err
	}
	h, err := s.households.GetByID(ctx, householdID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrHouseholdNotFound
		}
		return nil, err
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return nil, ErrEmptyName
		}
		h.Name = n
	}
	if in.GracePeriodDays != nil {
		if *in.GracePeriodDays < 0 {
			return nil, ErrInvalidGrace
		}
		h.GracePeriodDays = *in.GracePeriodDays
	}
	if err := s.households.Update(ctx, h); err != nil {
		return nil, fmt.Errorf("update household: %w", err)
	}
	return h, nil
}

// Dissolve soft-deletes a household. Owner-only. Membership rows remain so
// historical aggregates can still reference them per ADR-0007.
func (s *Service) Dissolve(ctx context.Context, userID, householdID int64) error {
	if err := s.requireOwner(ctx, userID, householdID); err != nil {
		return err
	}
	if err := s.households.SoftDelete(ctx, householdID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrHouseholdNotFound
		}
		return fmt.Errorf("dissolve household: %w", err)
	}
	return nil
}

// --- Invites ---

// CreateInvite mints a token for the household. Owner-only.
// The signup_mode check is defense in depth — invites work regardless of mode
// per ADR-0007, but we refuse if the instance was never bootstrapped at all.
func (s *Service) CreateInvite(ctx context.Context, userID, householdID int64, in CreateInviteInput) (*CreateInviteResult, error) {
	if err := s.requireOwner(ctx, userID, householdID); err != nil {
		return nil, err
	}
	if _, err := s.config.Get(ctx); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInstanceNotReady
		}
		return nil, fmt.Errorf("get instance_config: %w", err)
	}
	role := strings.TrimSpace(in.Role)
	if role == "" {
		role = model.RoleContributor
	}
	if !validRole(role) {
		return nil, ErrInvalidRole
	}

	token, err := auth.NewToken()
	if err != nil {
		return nil, fmt.Errorf("mint token: %w", err)
	}
	now := s.now()
	inv := &model.HouseholdInvite{
		HouseholdID: householdID,
		TokenHash:   auth.HashToken(token, s.secret),
		Role:        role,
		CreatedBy:   userID,
		ExpiresAt:   now.Add(InviteTTL),
		CreatedAt:   now,
	}
	if err := s.invites.Create(ctx, inv); err != nil {
		return nil, fmt.Errorf("create invite: %w", err)
	}
	return &CreateInviteResult{Invite: inv, Token: token}, nil
}

// AcceptInvite consumes a raw invite token for the calling user.
// Auto-resume per ADR-0007: if the user has a not-yet-purged left_at row in
// the same household, we clear left_at and return resumed=true. Otherwise we
// enforce the at-most-one-household invariant and create a fresh row.
func (s *Service) AcceptInvite(ctx context.Context, userID int64, rawToken string) (*AcceptResult, error) {
	if rawToken == "" {
		return nil, ErrInviteNotFound
	}
	inv, err := s.invites.GetByTokenHash(ctx, auth.HashToken(rawToken, s.secret))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInviteNotFound
		}
		return nil, err
	}
	now := s.now()
	if !inv.ExpiresAt.After(now) {
		return nil, ErrInviteExpired
	}
	if inv.AcceptedAt != nil {
		// If the same user is re-accepting their own previously consumed token
		// AND they have a left_at-marked row, that's the auto-resume path.
		if inv.AcceptedBy != nil && *inv.AcceptedBy == userID {
			if resumed, err := s.tryResume(ctx, userID, inv.HouseholdID); err == nil {
				h, hErr := s.households.GetByID(ctx, inv.HouseholdID)
				if hErr != nil {
					return nil, hErr
				}
				return &AcceptResult{Household: h, Member: resumed, Resumed: true}, nil
			}
		}
		return nil, ErrInviteAlreadyAccepted
	}

	// If the user already has a not-yet-purged row in this household, resume.
	if resumed, err := s.tryResume(ctx, userID, inv.HouseholdID); err == nil {
		if err := s.invites.MarkAccepted(ctx, inv.ID, userID, now); err != nil {
			return nil, fmt.Errorf("mark invite accepted: %w", err)
		}
		h, err := s.households.GetByID(ctx, inv.HouseholdID)
		if err != nil {
			return nil, err
		}
		return &AcceptResult{Household: h, Member: resumed, Resumed: true}, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	// Fresh join — at-most-one-household enforced across all households.
	if existing, err := s.members.GetMembershipForUser(ctx, userID); err == nil && existing != nil {
		return nil, ErrAlreadyMember
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	m := &model.HouseholdMember{
		HouseholdID: inv.HouseholdID,
		UserID:      userID,
		Role:        inv.Role,
		JoinedAt:    now,
	}
	if err := s.members.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("create member: %w", err)
	}
	if err := s.invites.MarkAccepted(ctx, inv.ID, userID, now); err != nil {
		return nil, fmt.Errorf("mark invite accepted: %w", err)
	}
	h, err := s.households.GetByID(ctx, inv.HouseholdID)
	if err != nil {
		return nil, err
	}
	return &AcceptResult{Household: h, Member: m, Resumed: false}, nil
}

// Leave is self-service: sets left_at on the caller's active membership.
// Returns ErrLastOwner if the caller is the sole remaining owner.
func (s *Service) Leave(ctx context.Context, userID, householdID int64) error {
	mem, err := s.members.GetActive(ctx, householdID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotMember
		}
		return err
	}
	if mem.Role == model.RoleOwner {
		n, err := s.members.CountActiveOwners(ctx, householdID)
		if err != nil {
			return fmt.Errorf("count owners: %w", err)
		}
		if n <= 1 {
			return ErrLastOwner
		}
	}
	if err := s.members.MarkLeft(ctx, mem.ID, s.now()); err != nil {
		return fmt.Errorf("mark left: %w", err)
	}
	return nil
}

// --- Account shares ---

// ListShares returns active shares for an account. Account-owner only.
func (s *Service) ListShares(ctx context.Context, userID, accountID int64) ([]model.AccountShare, error) {
	if err := s.requireAccountOwner(ctx, userID, accountID); err != nil {
		return nil, err
	}
	return s.shares.ListByAccount(ctx, accountID)
}

// SetShare upserts visibility for (accountID, householdID) or deletes the row
// when visibility == "private". Account-owner only.
func (s *Service) SetShare(ctx context.Context, userID, accountID, householdID int64, visibility string) (*model.AccountShare, error) {
	if err := s.requireAccountOwner(ctx, userID, accountID); err != nil {
		return nil, err
	}
	// Confirm the household actually exists; otherwise an attacker could fish
	// for valid household IDs via 404 vs 201 responses.
	if _, err := s.households.GetByID(ctx, householdID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrHouseholdNotFound
		}
		return nil, err
	}

	switch visibility {
	case model.VisibilityPrivate:
		if err := s.shares.SoftDelete(ctx, accountID, householdID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// Already private — idempotent success.
				return nil, nil
			}
			return nil, fmt.Errorf("clear share: %w", err)
		}
		return nil, nil
	case model.VisibilityBalanceOnly, model.VisibilityBalanceAndTxns:
		share, err := s.shares.Upsert(ctx, accountID, householdID, visibility)
		if err != nil {
			return nil, fmt.Errorf("upsert share: %w", err)
		}
		return share, nil
	default:
		return nil, ErrInvalidVisibility
	}
}

// --- internal helpers ---

func (s *Service) requireOwner(ctx context.Context, userID, householdID int64) error {
	mem, err := s.members.GetActive(ctx, householdID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotMember
		}
		return err
	}
	if mem.Role != model.RoleOwner {
		return ErrForbidden
	}
	return nil
}

func (s *Service) requireAccountOwner(ctx context.Context, userID, accountID int64) error {
	acc, err := s.accounts.GetByID(ctx, userID, accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrAccountNotOwned
		}
		return err
	}
	if acc.UserID != userID {
		return ErrAccountNotOwned
	}
	return nil
}

// tryResume looks for a not-yet-purged left_at-marked row in the given
// household for the user and clears left_at. Returns the resurrected member.
func (s *Service) tryResume(ctx context.Context, userID, householdID int64) (*model.HouseholdMember, error) {
	// We need ANY not-yet-purged row regardless of left_at. The active query
	// would skip a left-but-not-purged user — so query by user across the
	// household.
	mem, err := s.members.GetMembershipForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if mem.HouseholdID != householdID {
		// User is in a different household — at-most-one applies and there's
		// no resume path.
		return nil, repository.ErrNotFound
	}
	if mem.LeftAt == nil {
		// Already active; nothing to resume — but caller should treat this as
		// "you're already in, that's fine".
		return mem, nil
	}
	if err := s.members.ClearLeft(ctx, mem.ID); err != nil {
		return nil, err
	}
	mem.LeftAt = nil
	return mem, nil
}

func validRole(r string) bool {
	switch r {
	case model.RoleOwner, model.RoleContributor, model.RoleViewOnly:
		return true
	}
	return false
}
