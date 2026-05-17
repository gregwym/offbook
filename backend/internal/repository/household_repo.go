package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// HouseholdRepository owns CRUD for the households table itself.
type HouseholdRepository interface {
	Create(ctx context.Context, h *model.Household) error
	GetByID(ctx context.Context, id int64) (*model.Household, error)
	Update(ctx context.Context, h *model.Household) error
	SoftDelete(ctx context.Context, id int64) error
}

// HouseholdMemberRepository manages the membership lifecycle (join, leave,
// rejoin, purge). Reads return active rows by default; callers pass
// IncludeLeft / IncludePurged when they need lifecycle introspection.
type HouseholdMemberRepository interface {
	Create(ctx context.Context, m *model.HouseholdMember) error
	// GetActive returns the active (left_at IS NULL AND purged_at IS NULL) row
	// for (householdID, userID), or ErrNotFound.
	GetActive(ctx context.Context, householdID, userID int64) (*model.HouseholdMember, error)
	// GetByID returns a row by its primary key irrespective of lifecycle state.
	GetByID(ctx context.Context, id int64) (*model.HouseholdMember, error)
	// GetMembershipForUser returns the user's not-yet-purged row in any
	// household (active OR in-grace). At most one row by uniqueness invariant.
	GetMembershipForUser(ctx context.Context, userID int64) (*model.HouseholdMember, error)
	// ListActive returns all active members of a household. Order: role then id.
	ListActive(ctx context.Context, householdID int64) ([]model.HouseholdMember, error)
	// CountActiveOwners reports how many owners are still active (left_at IS NULL
	// AND purged_at IS NULL). Used to enforce the LAST_OWNER guard.
	CountActiveOwners(ctx context.Context, householdID int64) (int64, error)
	// MarkLeft sets left_at = now on a single membership row. Idempotent only
	// in the sense that it returns ErrNotFound if already left.
	MarkLeft(ctx context.Context, id int64, at time.Time) error
	// ClearLeft re-activates a not-yet-purged row by zeroing left_at.
	ClearLeft(ctx context.Context, id int64) error
}

// HouseholdInviteRepository persists invite tokens. Tokens are HMAC-hashed by
// the service before storage; only the raw token is given to the invitee.
type HouseholdInviteRepository interface {
	Create(ctx context.Context, i *model.HouseholdInvite) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*model.HouseholdInvite, error)
	MarkAccepted(ctx context.Context, id int64, userID int64, at time.Time) error
}

// AccountShareRepository owns the per-account, per-household visibility rows.
type AccountShareRepository interface {
	// GetActive returns the active row for (accountID, householdID), or ErrNotFound.
	GetActive(ctx context.Context, accountID, householdID int64) (*model.AccountShare, error)
	// ListByAccount returns active shares for the given account.
	ListByAccount(ctx context.Context, accountID int64) ([]model.AccountShare, error)
	// Upsert creates or updates the (accountID, householdID) share. If a
	// soft-deleted row exists for the same pair, it is resurrected with the
	// new visibility and a fresh updated_at.
	Upsert(ctx context.Context, accountID, householdID int64, visibility string) (*model.AccountShare, error)
	// SoftDelete clears the active share (sets visibility back to implicit
	// `private`). Returns ErrNotFound if no active row exists.
	SoftDelete(ctx context.Context, accountID, householdID int64) error
}

// --- households ---

type householdRepo struct{ db *gorm.DB }

func NewHouseholdRepository(db *gorm.DB) HouseholdRepository {
	return &householdRepo{db: db}
}

func (r *householdRepo) Create(ctx context.Context, h *model.Household) error {
	return r.db.WithContext(ctx).Create(h).Error
}

func (r *householdRepo) GetByID(ctx context.Context, id int64) (*model.Household, error) {
	var h model.Household
	if err := r.db.WithContext(ctx).First(&h, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &h, nil
}

func (r *householdRepo) Update(ctx context.Context, h *model.Household) error {
	res := r.db.WithContext(ctx).Save(h)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *householdRepo) SoftDelete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Household{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- household_members ---

type householdMemberRepo struct{ db *gorm.DB }

func NewHouseholdMemberRepository(db *gorm.DB) HouseholdMemberRepository {
	return &householdMemberRepo{db: db}
}

func (r *householdMemberRepo) Create(ctx context.Context, m *model.HouseholdMember) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *householdMemberRepo) GetActive(ctx context.Context, householdID, userID int64) (*model.HouseholdMember, error) {
	var m model.HouseholdMember
	err := r.db.WithContext(ctx).
		Where("household_id = ? AND user_id = ? AND left_at IS NULL AND purged_at IS NULL", householdID, userID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

func (r *householdMemberRepo) GetByID(ctx context.Context, id int64) (*model.HouseholdMember, error) {
	var m model.HouseholdMember
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

func (r *householdMemberRepo) GetMembershipForUser(ctx context.Context, userID int64) (*model.HouseholdMember, error) {
	var m model.HouseholdMember
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND purged_at IS NULL", userID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

func (r *householdMemberRepo) ListActive(ctx context.Context, householdID int64) ([]model.HouseholdMember, error) {
	var out []model.HouseholdMember
	err := r.db.WithContext(ctx).
		Where("household_id = ? AND left_at IS NULL AND purged_at IS NULL", householdID).
		Order("CASE role WHEN 'owner' THEN 0 WHEN 'contributor' THEN 1 ELSE 2 END, id").
		Find(&out).Error
	return out, err
}

func (r *householdMemberRepo) CountActiveOwners(ctx context.Context, householdID int64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&model.HouseholdMember{}).
		Where("household_id = ? AND role = ? AND left_at IS NULL AND purged_at IS NULL",
			householdID, model.RoleOwner).
		Count(&n).Error
	return n, err
}

func (r *householdMemberRepo) MarkLeft(ctx context.Context, id int64, at time.Time) error {
	res := r.db.WithContext(ctx).
		Model(&model.HouseholdMember{}).
		Where("id = ? AND left_at IS NULL AND purged_at IS NULL", id).
		Update("left_at", at)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *householdMemberRepo) ClearLeft(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).
		Model(&model.HouseholdMember{}).
		Where("id = ? AND purged_at IS NULL", id).
		Update("left_at", nil)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- household_invites ---

type householdInviteRepo struct{ db *gorm.DB }

func NewHouseholdInviteRepository(db *gorm.DB) HouseholdInviteRepository {
	return &householdInviteRepo{db: db}
}

func (r *householdInviteRepo) Create(ctx context.Context, i *model.HouseholdInvite) error {
	return r.db.WithContext(ctx).Create(i).Error
}

func (r *householdInviteRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*model.HouseholdInvite, error) {
	var i model.HouseholdInvite
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&i).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &i, nil
}

func (r *householdInviteRepo) MarkAccepted(ctx context.Context, id, userID int64, at time.Time) error {
	res := r.db.WithContext(ctx).
		Model(&model.HouseholdInvite{}).
		Where("id = ? AND accepted_at IS NULL", id).
		Updates(map[string]any{"accepted_at": at, "accepted_by": userID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- account_shares ---

type accountShareRepo struct{ db *gorm.DB }

func NewAccountShareRepository(db *gorm.DB) AccountShareRepository {
	return &accountShareRepo{db: db}
}

func (r *accountShareRepo) GetActive(ctx context.Context, accountID, householdID int64) (*model.AccountShare, error) {
	var s model.AccountShare
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND household_id = ?", accountID, householdID).
		First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *accountShareRepo) ListByAccount(ctx context.Context, accountID int64) ([]model.AccountShare, error) {
	var out []model.AccountShare
	err := r.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("household_id").
		Find(&out).Error
	return out, err
}

func (r *accountShareRepo) Upsert(ctx context.Context, accountID, householdID int64, visibility string) (*model.AccountShare, error) {
	// Look for any row — including soft-deleted — so we can resurrect rather than
	// collide with the partial unique index later.
	var s model.AccountShare
	err := r.db.WithContext(ctx).
		Unscoped().
		Where("account_id = ? AND household_id = ?", accountID, householdID).
		First(&s).Error
	switch {
	case err == nil:
		s.Visibility = visibility
		s.DeletedAt = gorm.DeletedAt{}
		if err := r.db.WithContext(ctx).Unscoped().Save(&s).Error; err != nil {
			return nil, err
		}
		return &s, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		s = model.AccountShare{
			AccountID:   accountID,
			HouseholdID: householdID,
			Visibility:  visibility,
		}
		if err := r.db.WithContext(ctx).Create(&s).Error; err != nil {
			return nil, err
		}
		return &s, nil
	default:
		return nil, err
	}
}

func (r *accountShareRepo) SoftDelete(ctx context.Context, accountID, householdID int64) error {
	res := r.db.WithContext(ctx).
		Where("account_id = ? AND household_id = ?", accountID, householdID).
		Delete(&model.AccountShare{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
