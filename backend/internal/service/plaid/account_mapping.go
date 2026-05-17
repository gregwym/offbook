package plaid

import "strings"

// MapAccountType collapses Plaid's (type, subtype) into the internal
// account_type enum allowed by the accounts table CHECK constraint:
// checking | savings | credit_card | loan | investment | crypto | cash | other.
//
// Inputs are case-insensitive. Subtype takes precedence when it's
// recognized — Plaid sometimes reports type="depository" with a more
// specific subtype like "money market" that maps to checking-equivalents.
//
// Unknown combinations fall through to "other" so a new Plaid subtype
// doesn't silently break account creation.
func MapAccountType(plaidType, plaidSubtype string) string {
	t := strings.ToLower(strings.TrimSpace(plaidType))
	s := strings.ToLower(strings.TrimSpace(plaidSubtype))

	// Subtype is the more precise signal; check it first when present.
	switch s {
	case "checking", "cash management":
		return "checking"
	case "savings", "cd", "money market", "hsa", "prepaid":
		return "savings"
	case "credit card", "paypal":
		return "credit_card"
	case "crypto exchange":
		return "crypto"
	}

	// Type-level fallbacks for subtypes we don't enumerate above.
	switch t {
	case "depository":
		return "checking" // safest depository default
	case "credit":
		return "credit_card"
	case "loan":
		return "loan"
	case "investment", "brokerage":
		return "investment"
	}
	return "other"
}
