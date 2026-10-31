package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// SavingsGoal is a named savings target owned by exactly one of a user
// (personal) or a household (shared) — ADR-0018. AccountID is only meaningful
// for a personal goal (a household goal spans members' shared accounts); the
// DB CHECK savings_goals_account_personal_chk enforces account_id ⇒ user-owned.
type SavingsGoal struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	UserID        *int64          `json:"user_id"`
	HouseholdID   *int64          `json:"household_id"`
	Name          string          `gorm:"not null" json:"name"`
	TargetAmount  decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"target_amount"`
	CurrentAmount decimal.Decimal `gorm:"type:numeric(30,18);not null;default:0" json:"current_amount"`
	TargetDate    *time.Time      `gorm:"type:date" json:"target_date,omitempty"`
	AccountID     *int64          `json:"account_id,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	DeletedAt     gorm.DeletedAt  `gorm:"index" json:"-"`

	Account *Account `gorm:"foreignKey:AccountID" json:"-"`
}

func (SavingsGoal) TableName() string { return "savings_goals" }

// SavingsGoalContribution is one recorded change to a goal's total (#682):
// a contribution (Amount > 0) or withdrawal (Amount < 0), with the goal's
// total right after it. Append-only. UserID is who recorded it; it is not
// exposed in the API (per-member attribution is #382).
type SavingsGoalContribution struct {
	ID           int64           `gorm:"primaryKey" json:"id"`
	GoalID       int64           `gorm:"not null" json:"goal_id"`
	UserID       *int64          `json:"-"`
	Amount       decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	BalanceAfter decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"balance_after"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (SavingsGoalContribution) TableName() string { return "savings_goal_contributions" }
