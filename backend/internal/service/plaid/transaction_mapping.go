package plaid

import (
	"fmt"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
)

// MapPlaidTransaction reshapes a Plaid transaction into our row format.
//
// Sign convention: Plaid returns positive amounts for money LEAVING the
// account (a $5 coffee charge is +5.00). The project's existing manual-
// entry conventions and tests treat negative as outflow / positive as
// inflow. Flip the sign at the boundary so the rest of the codebase
// (budgets, dashboard, etc.) sees a single consistent convention.
//
// Date convention: Plaid surfaces a single `date` (effective/posted) and
// an optional `authorized_date` (when the user initiated). We map:
//   - transaction_date ← Plaid `date`     (effective date — always present)
//   - posted_date      ← Plaid `authorized_date`
//
// This matches issue #61's spec; the framing was a deliberate product
// choice (effective date drives chronology; authorized_date is shown as
// "originally swiped on" metadata).
//
// userID + accountID are supplied by the service after resolving the
// session user and matching the Plaid account_id to a local accounts.id.
func MapPlaidTransaction(p PlaidTransaction, userID, accountID int64) (model.Transaction, error) {
	if p.PlaidTransactionID == "" {
		return model.Transaction{}, fmt.Errorf("plaid: transaction_id empty")
	}
	if p.PlaidAccountID == "" {
		return model.Transaction{}, fmt.Errorf("plaid: account_id empty for transaction %s", p.PlaidTransactionID)
	}

	txDate, err := time.Parse("2006-01-02", p.Date)
	if err != nil {
		return model.Transaction{}, fmt.Errorf("plaid: parse date %q: %w", p.Date, err)
	}

	var postedDate *time.Time
	if p.AuthorizedDate != nil {
		pd, err := time.Parse("2006-01-02", *p.AuthorizedDate)
		if err != nil {
			return model.Transaction{}, fmt.Errorf("plaid: parse authorized_date %q: %w", *p.AuthorizedDate, err)
		}
		postedDate = &pd
	}

	plaidID := p.PlaidTransactionID
	externalID := plaidID // mirror so the project's external_id dedup index covers this too
	source := "plaid"

	var description *string
	if p.Name != "" {
		v := p.Name
		description = &v
	}

	return model.Transaction{
		UserID:             userID,
		AccountID:          accountID,
		Amount:             p.Amount.Neg(), // sign flip, see header
		Currency:           p.Currency,
		Description:        description,
		MerchantName:       p.MerchantName,
		TransactionDate:    txDate,
		PostedDate:         postedDate,
		Source:             source,
		ExternalID:         &externalID,
		PlaidTransactionID: &plaidID,
	}, nil
}
