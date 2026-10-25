package model

import (
	"strings"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/merchant"
)

// BeforeCreate auto-resolves PrimaryQuoteAssetID from Currency when the
// caller leaves the FK at its zero value. Mirrors what the Phase-1 SQL
// trigger (000013) did, but at the model layer so it's discoverable in
// Go and survives the drop of the DB trigger in 000014. Production write
// paths (AccountService.Create, Plaid sync) set the FK explicitly; this
// hook covers the long tail of direct gorm.Create calls in tests and
// any future surface that forgets to populate it.
//
// Defensive: if the asset for the given currency doesn't exist, create
// it. Matches the trigger's behavior on first-encounter currencies.
func (a *Account) BeforeCreate(tx *gorm.DB) error {
	if a.PrimaryQuoteAssetID != 0 {
		return nil
	}
	symbol := a.Currency
	if symbol == "" {
		symbol = "USD"
	}
	asset, err := ensureFiatAsset(tx, symbol)
	if err != nil {
		return err
	}
	a.PrimaryQuoteAssetID = asset.ID
	return nil
}

// BeforeCreate populates PrimaryCurrencyAssetID with the USD asset id
// when not set. Same rationale as Account.BeforeCreate.
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.PrimaryCurrencyAssetID != 0 {
		return nil
	}
	asset, err := ensureFiatAsset(tx, "USD")
	if err != nil {
		return err
	}
	u.PrimaryCurrencyAssetID = asset.ID
	return nil
}

// BeforeCreate fills AssetID from the parent account's primary quote
// asset when the caller didn't set it. Cash transactions inherit their
// asset from the account; trade legs (#238) set the asset explicitly.
//
// It also derives DescriptionClean (#588) for a flow row that has a bank
// description but no merchant name, so merchant reports, recurring
// detection and the list group "SQ *BLUE BTL 0412" with "… 0415". Every
// insert path (manual, CSV, statement, Plaid) goes through here.
func (t *Transaction) BeforeCreate(tx *gorm.DB) error {
	fillDescriptionClean(t)
	if t.AssetID != 0 || t.AccountID == 0 {
		return nil
	}
	var pqaid int64
	if err := tx.Raw(`SELECT primary_quote_asset_id FROM accounts WHERE id = ?`, t.AccountID).Scan(&pqaid).Error; err != nil {
		return err
	}
	t.AssetID = pqaid
	return nil
}

// fillDescriptionClean sets DescriptionClean from Description when the
// caller left it unset and the row has no merchant name. Trade legs and
// reconciliation rows are not merchant spend and stay as they are.
func fillDescriptionClean(t *Transaction) {
	if t.DescriptionClean != nil || t.Description == nil || strings.TrimSpace(*t.Description) == "" {
		return
	}
	if t.MerchantName != nil && strings.TrimSpace(*t.MerchantName) != "" {
		return
	}
	if t.Kind != "" && t.Kind != KindFlow {
		return
	}
	clean := merchant.CleanDescription(*t.Description)
	t.DescriptionClean = &clean
}

// ensureFiatAsset looks up the fiat asset for symbol, creating it on
// first encounter. Reused by both BeforeCreate hooks above.
func ensureFiatAsset(tx *gorm.DB, symbol string) (*Asset, error) {
	var a Asset
	err := tx.Where("symbol = ? AND kind = ?", symbol, AssetKindFiat).First(&a).Error
	if err == nil {
		return &a, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	a = Asset{Symbol: symbol, Kind: AssetKindFiat, DisplayName: &symbol, Precision: 2}
	if err := tx.Create(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}
