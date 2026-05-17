package household

import "errors"

// Domain errors. Handlers map these to HTTP status codes; tests use errors.Is.
var (
	ErrHouseholdNotFound     = errors.New("household not found")
	ErrInviteNotFound        = errors.New("invite not found")
	ErrInviteExpired         = errors.New("invite expired")
	ErrInviteAlreadyAccepted = errors.New("invite already accepted")

	ErrEmptyName         = errors.New("name must not be empty")
	ErrInvalidRole       = errors.New("invalid role")
	ErrInvalidGrace      = errors.New("grace_period_days must be >= 0")
	ErrInvalidVisibility = errors.New("invalid visibility")

	// Lifecycle / authorization
	ErrForbidden        = errors.New("forbidden")
	ErrNotMember        = errors.New("not a member of this household")
	ErrAlreadyMember    = errors.New("already a member of a household")
	ErrLastOwner        = errors.New("cannot leave: you are the last owner")
	ErrAccountNotOwned  = errors.New("account does not belong to user")
	ErrInstanceNotReady = errors.New("instance not configured")
)
