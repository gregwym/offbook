package model

import "time"

// RuleApplyRun is one committed "Apply rules to existing transactions" run
// (#565). Its RuleApplyChange rows hold the prior state of every transaction
// the run changed, so the run can be undone. Append-only audit: never
// soft-deleted; UndoneAt marks a reverted run.
type RuleApplyRun struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	UserID       int64      `gorm:"not null" json:"-"`
	Scope        string     `gorm:"not null" json:"scope"`
	RowsChanged  int        `gorm:"not null" json:"rows_changed"`
	CreatedAt    time.Time  `json:"created_at"`
	UndoneAt     *time.Time `json:"undone_at,omitempty"`
	RowsRestored *int       `json:"rows_restored,omitempty"`
	RowsKept     *int       `json:"rows_kept,omitempty"`
}

func (RuleApplyRun) TableName() string { return "rule_apply_runs" }

// RuleApplyChange is a transaction's state before a rule apply run changed
// it. AppliedUpdatedAt is the updated_at the run wrote; undo restores the row
// only while its updated_at still equals it.
type RuleApplyChange struct {
	ID                       int64 `gorm:"primaryKey"`
	RunID                    int64 `gorm:"not null"`
	TransactionID            int64 `gorm:"not null"`
	PrevCategoryID           *int64
	PrevCategorizationMethod *string
	PrevCategorizationRuleID *int64
	PrevMerchantName         *string
	PrevHiddenAt             *time.Time
	PrevReviewedAt           *time.Time
	PrevTags                 Tags      `gorm:"type:text[];not null;default:'{}'"`
	AppliedUpdatedAt         time.Time `gorm:"not null"`
}

func (RuleApplyChange) TableName() string { return "rule_apply_changes" }

// ChangeFromTransaction snapshots the fields a rule apply can change.
func ChangeFromTransaction(t *Transaction) RuleApplyChange {
	tags := make(Tags, len(t.Tags))
	copy(tags, t.Tags)
	return RuleApplyChange{
		TransactionID:            t.ID,
		PrevCategoryID:           t.CategoryID,
		PrevCategorizationMethod: t.CategorizationMethod,
		PrevCategorizationRuleID: t.CategorizationRuleID,
		PrevMerchantName:         t.MerchantName,
		PrevHiddenAt:             t.HiddenAt,
		PrevReviewedAt:           t.ReviewedAt,
		PrevTags:                 tags,
	}
}
