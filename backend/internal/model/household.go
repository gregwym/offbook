package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Household roles. Only owners can invite, change grace period, or dissolve.
const (
	RoleOwner       = "owner"
	RoleContributor = "contributor"
	RoleViewOnly    = "view_only"
)

// Account share visibility levels. Absence of an account_shares row = "private".
const (
	VisibilityBalanceOnly     = "balance_only"
	VisibilityBalanceAndTxns  = "balance_and_txns"
)

type Household struct {
	ID               int64          `gorm:"primaryKey" json:"id"`
	Name             string         `gorm:"not null" json:"name"`
	OwnerID          int64          `gorm:"not null" json:"owner_id"`
	GracePeriodDays  int            `gorm:"not null;default:30" json:"grace_period_days"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Household) TableName() string { return "households" }

// HouseholdMember tracks a user's membership lifecycle. left_at marks the start
// of grace; purged_at marks final removal. See ADR-0007.
type HouseholdMember struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	HouseholdID int64      `gorm:"not null" json:"household_id"`
	UserID      int64      `gorm:"not null" json:"user_id"`
	Role        string     `gorm:"not null" json:"role"`
	JoinedAt    time.Time  `json:"joined_at"`
	LeftAt      *time.Time `json:"left_at,omitempty"`
	PurgedAt    *time.Time `json:"purged_at,omitempty"`
}

func (HouseholdMember) TableName() string { return "household_members" }

type HouseholdInvite struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	HouseholdID int64      `gorm:"not null" json:"household_id"`
	TokenHash   string     `gorm:"not null" json:"-"`
	Role        string     `gorm:"not null" json:"role"`
	CreatedBy   int64      `gorm:"not null" json:"created_by"`
	ExpiresAt   time.Time  `gorm:"not null" json:"expires_at"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
	AcceptedBy  *int64     `json:"accepted_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (HouseholdInvite) TableName() string { return "household_invites" }

type AccountShare struct {
	ID          int64          `gorm:"primaryKey" json:"id"`
	AccountID   int64          `gorm:"not null" json:"account_id"`
	HouseholdID int64          `gorm:"not null" json:"household_id"`
	Visibility  string         `gorm:"not null" json:"visibility"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (AccountShare) TableName() string { return "account_shares" }

// SharedBudget mirrors Budget but is owned by a household. No CRUD in M2.5.
type SharedBudget struct {
	ID           int64           `gorm:"primaryKey" json:"id"`
	HouseholdID  int64           `gorm:"not null" json:"household_id"`
	CategoryID   int64           `gorm:"not null" json:"category_id"`
	Period       string          `gorm:"not null" json:"period"`
	Amount       decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Rollover     bool            `gorm:"not null;default:false" json:"rollover"`
	IsActive     bool            `gorm:"not null;default:true" json:"is_active"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	DeletedAt    gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (SharedBudget) TableName() string { return "shared_budgets" }

// SharedGoal mirrors SavingsGoal but is owned by a household. No CRUD in M2.5.
type SharedGoal struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	HouseholdID   int64           `gorm:"not null" json:"household_id"`
	Name          string          `gorm:"not null" json:"name"`
	TargetAmount  decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"target_amount"`
	CurrentAmount decimal.Decimal `gorm:"type:numeric(30,18);not null;default:0" json:"current_amount"`
	TargetDate    *time.Time      `gorm:"type:date" json:"target_date,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	DeletedAt     gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (SharedGoal) TableName() string { return "shared_goals" }
