package repository

import "gorm.io/gorm"

// PlanOwner scopes a budget or savings goal to exactly one owner — a personal
// book (UserID) or a household (HouseholdID), never both. It is the data-layer
// expression of ADR-0018: budgets and savings_goals are single tables whose
// rows carry one owner, with a DB CHECK enforcing the XOR. Repositories filter
// reads/writes through Scope so a personal caller can never touch a household
// row and vice versa.
type PlanOwner struct {
	UserID      *int64
	HouseholdID *int64
}

// UserOwner builds a personal-scope owner.
func UserOwner(userID int64) PlanOwner { return PlanOwner{UserID: &userID} }

// HouseholdOwner builds a household-scope owner.
func HouseholdOwner(householdID int64) PlanOwner { return PlanOwner{HouseholdID: &householdID} }

// Valid reports whether exactly one owner is set (mirrors the DB CHECK).
func (o PlanOwner) Valid() bool {
	return (o.UserID != nil) != (o.HouseholdID != nil)
}

// Apply narrows a query to rows owned by this owner. The non-owning column is
// asserted NULL so a personal filter can't match a household row that happens
// to share an id space, and vice versa.
func (o PlanOwner) Apply(db *gorm.DB) *gorm.DB {
	if o.UserID != nil {
		return db.Where("user_id = ? AND household_id IS NULL", *o.UserID)
	}
	if o.HouseholdID != nil {
		return db.Where("household_id = ? AND user_id IS NULL", *o.HouseholdID)
	}
	// No owner set — match nothing rather than leak every row.
	return db.Where("1 = 0")
}
