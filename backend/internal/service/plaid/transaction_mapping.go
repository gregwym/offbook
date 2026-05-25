package plaid

import (
	"fmt"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
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
// assetID is the parent account's primary_quote_asset_id — every cash
// transaction inherits it. Trade-pair ingestion (#238) will override per
// leg.
//
// The mapper is optional (nil = no Plaid-PFC fallback). User rules win
// over Plaid defaults: if any compiled rule matches, CategoryID gets the
// rule's category, CategorizationMethod is "rule", and CategorizationRuleID
// records which rule fired. Otherwise we fall through to the Plaid PFC
// mapper (CategorizationMethod="plaid_default"). User-edited values always
// win on subsequent updates — see MergePlaidUpdate.
func MapPlaidTransaction(p PlaidTransaction, userID, accountID, assetID int64, mapper *CategoryMapper, rules []categorization.CompiledRule) (model.Transaction, error) {
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

	out := model.Transaction{
		UserID:             userID,
		AccountID:          accountID,
		AssetID:            assetID,
		Amount:             p.Amount.Neg(), // sign flip, see header
		Description:        description,
		MerchantName:       p.MerchantName,
		TransactionDate:    txDate,
		PostedDate:         postedDate,
		Source:             source,
		ExternalID:         &externalID,
		PlaidTransactionID: &plaidID,
	}

	// Rules first — a matching rule overrides the Plaid PFC default.
	if _, applied := categorization.Apply(&out, rules); !applied {
		if catID, ok := mapper.MapPlaidCategory(p.PFCPrimary, p.PFCDetailed); ok {
			out.CategoryID = &catID
			method := CategorizationMethodPlaidDefault
			out.CategorizationMethod = &method
		}
	}
	return out, nil
}

// MergePlaidUpdate overlays an incoming /transactions/sync `modified` entry
// onto the existing row. Plaid-controlled fields (amount, description,
// merchant_name, posted_date, transaction_date) are taken from the new
// payload; user-edited fields (notes, category_id, is_transfer, transfer
// pairing) are preserved untouched.
//
// This is what makes "pending → posted" transitions safe: Plaid updates the
// amount and posted_date when a hold clears, but the user's categorization
// and notes survive.
//
// The function is pure — it returns the merged row without writing. Caller
// passes the result to the repo's update method. accountID is supplied
// (rather than re-derived) so the merge stays independent of repo state.
func MergePlaidUpdate(existing model.Transaction, incoming PlaidTransaction, accountID, assetID int64, mapper *CategoryMapper, rules []categorization.CompiledRule) (model.Transaction, error) {
	mapped, err := MapPlaidTransaction(incoming, existing.UserID, accountID, assetID, mapper, rules)
	if err != nil {
		return model.Transaction{}, err
	}
	merged := existing
	// Plaid-owned fields — overwrite.
	merged.AccountID = accountID
	merged.AssetID = mapped.AssetID
	merged.Amount = mapped.Amount
	merged.Description = mapped.Description
	merged.MerchantName = mapped.MerchantName
	merged.TransactionDate = mapped.TransactionDate
	merged.PostedDate = mapped.PostedDate
	// Category: a non-null existing CategoryID is sacred — that's either
	// the user's manual pick, a previously-fired rule, or a prior
	// plaid_default that the user has implicitly accepted. Only fill from
	// the rule engine / mapper when the row has no category yet.
	if existing.CategoryID == nil && mapped.CategoryID != nil {
		merged.CategoryID = mapped.CategoryID
		merged.CategorizationMethod = mapped.CategorizationMethod
		merged.CategorizationRuleID = mapped.CategorizationRuleID
	}
	// User-edited fields — preserve. Intentionally not enumerated as
	// "deny-list overlay" because the safer default for unknown future
	// fields is "leave it alone".
	//   existing.Notes, existing.IsTransfer, existing.TransferPairID
	// remain on `merged`.
	return merged, nil
}
