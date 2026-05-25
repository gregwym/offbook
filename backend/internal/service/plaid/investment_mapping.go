package plaid

import (
	"fmt"
	"strings"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
)

// InvestmentAction is the decoded intent of one Plaid investment-
// transaction row. The service applies it inside its DB transaction:
//
//   - ActionPair → writes both legs via CreateTradePair.
//   - ActionSingleCash → writes only CashLeg.
//   - ActionCancel → soft-deletes a previously-ingested row keyed by
//     CancelPlaidTransactionID (and its partner, if paired).
//   - ActionIgnore → no-op (fee-on-cash with zero amount, unknown
//     subtype with no actionable signal).
type InvestmentAction int

const (
	ActionIgnore InvestmentAction = iota
	ActionPair
	ActionSingleCash
	ActionCancel
)

// InvestmentPlan is the structured output of MapInvestmentTransaction.
// Exposes only the fields the service needs to act — keeps the mapper
// independent of repository and gorm types.
type InvestmentPlan struct {
	Action InvestmentAction

	// Populated for ActionPair and ActionSingleCash. Security and Cash
	// share TransactionDate, Source, PlaidTransactionID, ExternalID.
	Security *model.Transaction // nil when ActionSingleCash
	Cash     *model.Transaction

	// Populated for ActionCancel — the Plaid id of the row being
	// cancelled. Service soft-deletes by this id + clears its partner.
	CancelPlaidTransactionID string
}

// MapInvestmentTransaction translates one Plaid investment-transaction
// row into an InvestmentPlan. The mapper is pure: no DB, no I/O. The
// asset id for the security leg is resolved by the caller via
// assetRepo.EnsureBySymbolKind ahead of time — passed as securityAssetID.
// For non-security legs (cash, fee, dividend without security_id),
// securityAssetID may be 0.
//
// Sign convention from Plaid (per the API docs):
//   - Amount  > 0  → money LEFT the account (e.g. buy, fee)
//   - Quantity > 0 → shares ENTERED the account (e.g. buy)
//
// Project convention is the opposite for Amount (positive = inflow).
// We sign-flip Amount at the boundary so downstream (budgets,
// dashboard, AI) sees one consistent convention.
func MapInvestmentTransaction(
	pit PlaidInvestmentTransaction,
	userID, accountID, cashAssetID, securityAssetID int64,
) (InvestmentPlan, error) {
	if pit.PlaidTransactionID == "" {
		return InvestmentPlan{}, fmt.Errorf("plaid: investment transaction_id empty")
	}
	if pit.PlaidAccountID == "" {
		return InvestmentPlan{}, fmt.Errorf("plaid: account_id empty for investment txn %s", pit.PlaidTransactionID)
	}

	if strings.EqualFold(pit.Type, "cancel") {
		if pit.CancelTransactionID == nil || *pit.CancelTransactionID == "" {
			// Cancel row with no target — nothing to undo. Treat as
			// ignore rather than error so the cursor still advances.
			return InvestmentPlan{Action: ActionIgnore}, nil
		}
		return InvestmentPlan{
			Action:                   ActionCancel,
			CancelPlaidTransactionID: *pit.CancelTransactionID,
		}, nil
	}

	txDate, err := time.Parse("2006-01-02", pit.Date)
	if err != nil {
		return InvestmentPlan{}, fmt.Errorf("plaid: parse investment date %q: %w", pit.Date, err)
	}

	plaidID := pit.PlaidTransactionID
	externalID := plaidID
	source := "plaid"
	desc := pit.Name
	descPtr := &desc

	// Cash leg amount in project convention: Plaid's amount is positive
	// when money leaves the account; we store negative for outflow.
	cashAmount := pit.Amount.Neg()

	switch strings.ToLower(pit.Type) {
	case "buy", "sell":
		if securityAssetID == 0 {
			return InvestmentPlan{}, fmt.Errorf("plaid: %s row has no resolvable security (txn %s)", pit.Type, plaidID)
		}
		// Plaid Quantity is positive for buy, negative for sell already —
		// matches the project's "positive = inflow into the account"
		// convention for the security leg. No sign flip on quantity.
		securityAmount := pit.Quantity
		sec := &model.Transaction{
			UserID:             userID,
			AccountID:          accountID,
			AssetID:            securityAssetID,
			Amount:             securityAmount,
			Description:        descPtr,
			TransactionDate:    txDate,
			Source:             source,
			ExternalID:         strPtr(externalID + ":sec"),
			PlaidTransactionID: strPtr(plaidID),
		}
		cash := &model.Transaction{
			UserID:             userID,
			AccountID:          accountID,
			AssetID:            cashAssetID,
			Amount:             cashAmount,
			Description:        descPtr,
			TransactionDate:    txDate,
			Source:             source,
			ExternalID:         strPtr(externalID + ":cash"),
			PlaidTransactionID: strPtr(plaidID + ":cash"),
		}
		return InvestmentPlan{Action: ActionPair, Security: sec, Cash: cash}, nil

	case "cash", "fee", "transfer":
		// Single cash leg — dividends, interest, account fees, cash
		// transfers in/out. The plaid_transaction_id stays canonical so
		// future modifies/cancels still address this row.
		cash := &model.Transaction{
			UserID:             userID,
			AccountID:          accountID,
			AssetID:            cashAssetID,
			Amount:             cashAmount,
			Description:        descPtr,
			TransactionDate:    txDate,
			Source:             source,
			ExternalID:         strPtr(externalID),
			PlaidTransactionID: strPtr(plaidID),
		}
		return InvestmentPlan{Action: ActionSingleCash, Cash: cash}, nil

	default:
		// Unknown type — skip rather than error so a future Plaid type
		// rollout doesn't poison every sync. The DLQ in service.go
		// catches genuine mapping errors raised above.
		return InvestmentPlan{Action: ActionIgnore}, nil
	}
}

// SecurityKind maps Plaid's `securities.type` taxonomy to the project's
// asset kind enum (model.AssetKind*). Falls back to "other" for values
// we don't recognize — keeps the asset row writable without erroring.
func SecurityKind(plaidType string, isCashEquivalent bool) string {
	if isCashEquivalent {
		return model.AssetKindFiat
	}
	switch strings.ToLower(strings.TrimSpace(plaidType)) {
	case "equity":
		return model.AssetKindEquity
	case "mutual fund", "etf":
		return model.AssetKindFund
	case "fixed income":
		return model.AssetKindBond
	case "cryptocurrency":
		return model.AssetKindCrypto
	case "cash":
		return model.AssetKindFiat
	default:
		return model.AssetKindOther
	}
}

// SecuritySymbol picks the best identifier for an asset row. Prefers
// the ticker; falls back to the security name. Empty string when both
// are blank (the caller should treat as an unmappable security).
func SecuritySymbol(s PlaidSecurity) string {
	if v := strings.TrimSpace(s.TickerSymbol); v != "" {
		return v
	}
	return strings.TrimSpace(s.Name)
}
