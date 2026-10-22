package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Budget is a per-category spend limit per period, owned by exactly one of a
// user (personal) or a household (shared) — ADR-0018. The DB CHECK
// budgets_owner_chk enforces the XOR; repository.PlanOwner is the data-layer
// counterpart. Exactly one of UserID / HouseholdID is non-nil.
type Budget struct {
	ID          int64           `gorm:"primaryKey" json:"id"`
	UserID      *int64          `json:"user_id"`
	HouseholdID *int64          `json:"household_id"`
	CategoryID  int64           `gorm:"not null" json:"category_id"`
	Period      string          `gorm:"not null" json:"period"`
	Amount      decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Rollover    bool            `gorm:"not null;default:false" json:"rollover"`
	// RolloverStart seeds the carryover in the creation period (#508); >= 0,
	// ignored unless Rollover.
	RolloverStart decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"rollover_start"`
	// IsActive: the DB column has DEFAULT TRUE (migration 000001), but we
	// deliberately leave the GORM tag without `default:true` so explicit
	// `false` from the service layer survives INSERT — gorm omits zero-
	// valued fields when the tag carries a default clause.
	IsActive  bool           `gorm:"not null" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"-"`
}

func (Budget) TableName() string { return "budgets" }

// BudgetAmount is one entry in a budget's limit history (#580): Amount is the
// limit from EffectiveFrom (a period start, UTC date) until the next entry.
type BudgetAmount struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	BudgetID      int64           `gorm:"not null" json:"budget_id"`
	Amount        decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	EffectiveFrom time.Time       `gorm:"type:date;not null" json:"effective_from"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (BudgetAmount) TableName() string { return "budget_amounts" }
