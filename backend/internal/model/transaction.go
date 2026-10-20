package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Transaction kinds (ADR-0017). `kind` classifies a row for both quantity
// reconstruction (positions = Σ amount per account/asset) and flow analytics.
// Only KindFlow is counted by spending / cash-flow / budget aggregates;
// KindOpeningBalance and KindAdjustment are quantity facts but not flow.
const (
	KindFlow           = "flow"
	KindTradeLeg       = "trade_leg"
	KindOpeningBalance = "opening_balance"
	KindAdjustment     = "adjustment"
)

// Transaction is a movement of `Amount` units of `AssetID` in or out of
// `AccountID`. Per ADR-0013, `Amount` is the quantity (positive = in,
// negative = out) and `AssetID` is the unit. For cash transactions
// `AssetID` equals the parent account's primary_quote_asset. For trades
// (issue #238), the cash leg and security leg each carry their own
// `AssetID` and a shared `TransferPairID`. `Kind` (ADR-0017) classifies
// the row; it defaults to KindFlow.
type Transaction struct {
	ID                   int64           `gorm:"primaryKey" json:"id"`
	UserID               int64           `gorm:"not null" json:"user_id"`
	AccountID            int64           `gorm:"not null" json:"account_id"`
	AssetID              int64           `gorm:"not null" json:"asset_id"`
	CategoryID           *int64          `json:"category_id,omitempty"`
	Kind                 string          `gorm:"not null;default:flow" json:"kind"`
	Amount               decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Description          *string         `json:"description,omitempty"`
	DescriptionClean     *string         `json:"description_clean,omitempty"`
	MerchantName         *string         `json:"merchant_name,omitempty"`
	TransactionDate      time.Time       `gorm:"type:date;not null" json:"transaction_date"`
	PostedDate           *time.Time      `gorm:"type:date" json:"posted_date,omitempty"`
	Source               string          `gorm:"not null" json:"source"`
	ExternalID           *string         `gorm:"column:external_id" json:"external_id,omitempty"`
	PlaidTransactionID   *string         `gorm:"column:plaid_transaction_id" json:"plaid_transaction_id,omitempty"`
	CategorizationMethod *string         `json:"categorization_method,omitempty"`
	CategorizationRuleID *int64          `json:"categorization_rule_id,omitempty"`
	IsTransfer           bool            `gorm:"not null;default:false" json:"is_transfer"`
	TransferPairID       *int64          `json:"transfer_pair_id,omitempty"`
	Notes                *string         `json:"notes,omitempty"`
	// CausedByObservationID links an opening_balance/adjustment row to the
	// account_balance_observations row that caused it (ADR-0017 §
	// reconciliation; #370). Audit linkage only — nil on ordinary flow rows
	// and on legacy reconciling rows written before this column existed.
	CausedByObservationID *int64 `json:"caused_by_observation_id,omitempty"`
	// AcknowledgedAt/AcknowledgedNote let a user mark an adjustment reviewed
	// (#370) without altering ledger fields. Only meaningful on kind=adjustment
	// rows; nil everywhere else.
	AcknowledgedAt   *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedNote *string    `json:"acknowledged_note,omitempty"`
	// ReviewedAt is explicit user review state (#424), independent of
	// categorization. NULL = unreviewed (appears in the review inbox); set =
	// a human confirmed it. Metadata only — never affects ledger/valuation.
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	// HiddenAt (#465) marks a flow row the user excluded from spending, income,
	// budgets and merchant reports. The ledger/balances are unaffected.
	HiddenAt *time.Time `json:"hidden_at,omitempty"`
	// Pending (#513) is true while the bank hasn't posted the row yet (Plaid
	// `pending`). Bank-owned: sync overwrites it; the pending -> posted
	// adoption clears it on the same row, keeping the user's edits.
	Pending bool `gorm:"not null;default:false" json:"pending"`
	// Tags (#544) are user labels ("vacation", "reimbursable"). Metadata
	// only — never affects ledger, spending or budgets. User-owned: sync
	// never touches them.
	Tags Tags `gorm:"type:text[];not null;default:'{}'" json:"tags"`
	// ImportJobID links a statement-imported row to the ingestion_jobs audit
	// row that created it, so the import can be undone as a unit.
	ImportJobID *int64         `json:"import_job_id,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	// Splits is populated by the repository on reads (#438); never persisted
	// through this struct. Empty = unsplit.
	Splits []TransactionSplit `gorm:"-" json:"splits,omitempty"`
	// SplitsNeedAttention (#445) is true when the splits no longer sum to the
	// amount (the bank revised it, e.g. Plaid pending -> posted). The splits
	// are kept for the user to re-save, but reporting falls back to the
	// parent row until then. Derived on read; never persisted.
	SplitsNeedAttention bool `gorm:"-" json:"splits_need_attention,omitempty"`

	Account      *Account     `gorm:"foreignKey:AccountID" json:"-"`
	Asset        *Asset       `gorm:"foreignKey:AssetID" json:"-"`
	Category     *Category    `gorm:"foreignKey:CategoryID" json:"-"`
	TransferPair *Transaction `gorm:"foreignKey:TransferPairID" json:"-"`
}

func (Transaction) TableName() string { return "transactions" }
