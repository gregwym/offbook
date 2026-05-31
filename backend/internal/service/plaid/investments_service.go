// Package plaid: investments surfaces — issue #238 part 2.
//
// SyncInvestmentTransactions and SyncHoldings extend the M3 cash-only
// sync to brokerage accounts. The cash-sleeve write path established by
// SyncTransactions is intentionally NOT reused: investment transactions
// don't ride the /transactions/sync cursor — Plaid exposes them via
// /investments/transactions/get keyed by a date range. Cancellations
// come through as their own rows with cancel_transaction_id pointing
// at the original.
package plaid

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// SyncInvestmentTransactionsResult summarizes one /investments/
// transactions/get drain.
//   - PairedInserted: paired buy/sell rows where BOTH legs landed.
//   - CashInserted: single-leg cash rows (dividends, interest, fees).
//   - Cancelled: cancel rows that removed a previously-ingested trade
//     pair or single leg.
//   - Skipped: rows the mapper deliberately ignored (e.g. unknown type).
//   - Failed: rows that hit a mapping error and DLQ'd to
//     plaid_sync_errors.
type SyncInvestmentTransactionsResult struct {
	PairedInserted int `json:"paired_inserted"`
	CashInserted   int `json:"cash_inserted"`
	Cancelled      int `json:"cancelled"`
	Skipped        int `json:"skipped"`
	Failed         int `json:"failed"`
}

// HoldingsReconcileResult summarizes a /investments/holdings/get
// reconciliation. PositionsAdjusted counts (account, asset) pairs
// where Plaid's snapshot quantity disagreed with our derived position
// — we wrote the Plaid number (per ADR-0013 §3) without inserting any
// synthetic transactions.
type HoldingsReconcileResult struct {
	Holdings          int `json:"holdings"`
	PositionsAdjusted int `json:"positions_adjusted"`
	PricesObserved    int `json:"prices_observed"`
}

// DefaultInvestmentLookback is the trailing window pulled when the
// caller doesn't supply one. Two years matches Plaid's max supported
// range for investments/transactions and is generous enough that an
// initial pull captures the user's tax-relevant history without
// piling on cost.
const DefaultInvestmentLookback = 730 * 24 * time.Hour

// SyncInvestmentTransactions drains /investments/transactions/get for
// the item over [from, to]. Mirrors SyncTransactions' two-phase
// approach (fetch all → one DB transaction with per-row savepoints).
//
// Per the "app never invents transactions" invariant: a row only lands
// if Plaid sent it. The cancel branch removes prior rows rather than
// generating reversing entries; the reconciliation path adjusts
// positions but does not synthesize trades.
func (s *Service) SyncInvestmentTransactions(ctx context.Context, userID int64, plaidItemID string, from, to time.Time) (result SyncInvestmentTransactionsResult, retErr error) {
	if !s.Configured() || s.acctRepo == nil || s.txRepo == nil || s.assetRepo == nil || s.positionRepo == nil || s.db == nil {
		return SyncInvestmentTransactionsResult{}, ErrNotConfigured
	}
	item, err := s.itemRepo.GetByPlaidItemID(ctx, userID, plaidItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return SyncInvestmentTransactionsResult{}, ErrItemNotFound
		}
		return SyncInvestmentTransactionsResult{}, fmt.Errorf("plaid: lookup item: %w", err)
	}

	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-DefaultInvestmentLookback)
	}

	tokenBytes, err := s.box.Decrypt(item.AccessTokenEnc)
	if err != nil {
		return SyncInvestmentTransactionsResult{}, fmt.Errorf("plaid: decrypt access_token: %w", err)
	}
	defer zeroBytes(tokenBytes)

	fetched, err := s.client.FetchInvestmentTransactions(ctx, string(tokenBytes), from, to)
	if err != nil {
		return SyncInvestmentTransactionsResult{}, err
	}

	// Resolve securities to assets up front, outside the DB transaction.
	// EnsureBySymbolKind creates rows on first encounter; we don't want
	// to do that inside a long-running savepoint loop.
	assetBySecurity := map[string]int64{}
	for _, sec := range fetched.Securities {
		symbol := SecuritySymbol(sec)
		if symbol == "" {
			continue
		}
		kind := SecurityKind(sec.Type, sec.IsCashEquivalent)
		display := sec.Name
		if display == "" {
			display = symbol
		}
		asset, err := s.assetRepo.EnsureBySymbolKind(ctx, symbol, kind, display)
		if err != nil {
			return SyncInvestmentTransactionsResult{}, fmt.Errorf("plaid: ensure asset %s/%s: %w", symbol, kind, err)
		}
		assetBySecurity[sec.PlaidSecurityID] = asset.ID
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		acctRepo := repository.NewAccountRepository(tx)
		posRepo := repository.NewPositionRepository(tx)
		syncErrRepo := repository.NewPlaidSyncErrorRepository(tx)
		accountIDCache := map[string]accountRef{}
		spIdx := 0

		// Track which (accountID, securityAssetID) pairs were touched so
		// we recompute positions/cost basis once at the end.
		type touchedKey struct{ accountID, assetID int64 }
		touched := map[touchedKey]struct{}{}

		withSavepoint := func(fn func() error) error {
			spIdx++
			name := fmt.Sprintf("plaid_inv_%d", spIdx)
			if err := tx.SavePoint(name).Error; err != nil {
				return fmt.Errorf("plaid: savepoint %s: %w", name, err)
			}
			if err := fn(); err != nil {
				if rErr := tx.RollbackTo(name).Error; rErr != nil {
					return fmt.Errorf("plaid: rollback to %s: %w (orig: %v)", name, rErr, err)
				}
				return err
			}
			return nil
		}
		recordDLQ := func(pit PlaidInvestmentTransaction, code string, cause error) error {
			row := &model.PlaidSyncError{
				UserID:             userID,
				PlaidItemID:        item.ID,
				PlaidTransactionID: strPtr(pit.PlaidTransactionID),
				RawPayload:         []byte(`{}`),
				ErrorCode:          code,
				ErrorMessage:       capErrorMessage(cause.Error()),
				OccurredAt:         time.Now().UTC(),
			}
			if err := syncErrRepo.Create(ctx, row); err != nil {
				return fmt.Errorf("plaid: write DLQ row: %w", err)
			}
			result.Failed++
			return nil
		}

		for _, pit := range fetched.Transactions {
			perRowErr := withSavepoint(func() error {
				ref, err := resolveAccount(ctx, acctRepo, userID, pit.PlaidAccountID, accountIDCache)
				if err != nil {
					return err
				}
				var securityAssetID int64
				if pit.PlaidSecurityID != nil {
					if id, ok := assetBySecurity[*pit.PlaidSecurityID]; ok {
						securityAssetID = id
					}
				}
				plan, err := MapInvestmentTransaction(pit, userID, ref.ID, ref.AssetID, securityAssetID)
				if err != nil {
					return err
				}
				switch plan.Action {
				case ActionIgnore:
					result.Skipped++
				case ActionPair:
					if err := txRepo.CreateTradePair(ctx, plan.Security, plan.Cash); err != nil {
						return err
					}
					result.PairedInserted++
					touched[touchedKey{ref.ID, plan.Security.AssetID}] = struct{}{}
				case ActionSingleCash:
					if _, err := txRepo.CreateBatch(ctx, []model.Transaction{*plan.Cash}); err != nil {
						return err
					}
					result.CashInserted++
				case ActionCancel:
					// Soft-delete the original row and (if it was paired)
					// its partner. Locate by plaid_transaction_id; the
					// pair-extension we wrote was "<orig>:cash".
					orig, err := lookupByPlaidTxnID(ctx, tx, userID, plan.CancelPlaidTransactionID)
					if err != nil {
						return err
					}
					if orig != nil {
						if orig.TransferPairID != nil {
							if err := tx.WithContext(ctx).Where("user_id = ?", userID).
								Delete(&model.Transaction{}, *orig.TransferPairID).Error; err != nil {
								return err
							}
						}
						if err := tx.WithContext(ctx).Where("user_id = ?", userID).
							Delete(&model.Transaction{}, orig.ID).Error; err != nil {
							return err
						}
						result.Cancelled++
						if orig.AssetID != ref.AssetID {
							touched[touchedKey{orig.AccountID, orig.AssetID}] = struct{}{}
						}
					}
				}
				return nil
			})
			if perRowErr != nil {
				if err := recordDLQ(pit, model.PlaidSyncErrorCodeMapping, perRowErr); err != nil {
					return err
				}
			}
		}

		// Recompute positions + cost basis for every touched (account,
		// asset) pair, then rebase each account's cash sleeve once. Same
		// canonical recompute the manual-trade flow uses; we don't trust
		// deltas.
		user, err := repository.NewUserRepository(tx).GetByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("plaid: load user: %w", err)
		}
		priceRepo := repository.NewPriceRepository(tx)
		cashTouched := map[int64]int64{} // accountID → cash asset id
		for key := range touched {
			res, err := valuation.Recompute(ctx, txRepo, priceRepo, userID, key.accountID, key.assetID, user.PrimaryCurrencyAssetID)
			if err != nil {
				if errors.Is(err, valuation.ErrFXUnavailable) {
					log.Printf("plaid: cost basis FX unavailable for (acct=%d asset=%d): %v", key.accountID, key.assetID, err)
				} else {
					return fmt.Errorf("recompute position: %w", err)
				}
			}
			pos := &model.Position{
				UserID:    userID,
				AccountID: key.accountID,
				AssetID:   key.assetID,
				Quantity:  res.Quantity,
			}
			if res.HasCostBasis {
				cb := res.CostBasis
				pos.CostBasis = &cb
			}
			if err := posRepo.Upsert(ctx, pos); err != nil {
				return fmt.Errorf("upsert position: %w", err)
			}
			// Note the account's cash asset so we rebase it below.
			if _, ok := cashTouched[key.accountID]; !ok {
				acct, err := acctRepo.GetByID(ctx, userID, key.accountID)
				if err != nil {
					return fmt.Errorf("load account for cash rebase: %w", err)
				}
				cashTouched[key.accountID] = acct.PrimaryQuoteAssetID
			}
		}
		// Cash sleeve rebase per touched account.
		for accountID, cashAssetID := range cashTouched {
			res, err := valuation.Recompute(ctx, txRepo, priceRepo, userID, accountID, cashAssetID, user.PrimaryCurrencyAssetID)
			if err != nil {
				return fmt.Errorf("recompute cash: %w", err)
			}
			pos := &model.Position{
				UserID:    userID,
				AccountID: accountID,
				AssetID:   cashAssetID,
				Quantity:  res.Quantity,
			}
			if err := posRepo.Upsert(ctx, pos); err != nil {
				return fmt.Errorf("upsert cash position: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return SyncInvestmentTransactionsResult{}, err
	}
	return result, nil
}

// lookupByPlaidTxnID fetches a transaction by user_id + plaid_transaction_id
// — used by the cancel path to find the row to soft-delete. Returns
// (nil, nil) when no row exists (idempotent cancel).
func lookupByPlaidTxnID(ctx context.Context, tx *gorm.DB, userID int64, plaidTxnID string) (*model.Transaction, error) {
	var t model.Transaction
	err := tx.WithContext(ctx).
		Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
		First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// SyncHoldings pulls /investments/holdings/get and reconciles each
// (account × security) snapshot row against our derived positions:
//   - quantity differs → adjust the position row (Plaid wins).
//   - cost_basis present and ours is null → adopt Plaid's value.
//   - institution_price → write a price observation tagged source='plaid'.
//
// Per ADR-0013 §3, snapshot disagreements DO NOT spawn synthetic
// transactions. The function emits a log warning on every adjustment
// so an operator review can spot drift.
func (s *Service) SyncHoldings(ctx context.Context, userID int64, plaidItemID string) (HoldingsReconcileResult, error) {
	if !s.Configured() || s.acctRepo == nil || s.assetRepo == nil || s.positionRepo == nil || s.db == nil {
		return HoldingsReconcileResult{}, ErrNotConfigured
	}
	item, err := s.itemRepo.GetByPlaidItemID(ctx, userID, plaidItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return HoldingsReconcileResult{}, ErrItemNotFound
		}
		return HoldingsReconcileResult{}, fmt.Errorf("plaid: lookup item: %w", err)
	}
	tokenBytes, err := s.box.Decrypt(item.AccessTokenEnc)
	if err != nil {
		return HoldingsReconcileResult{}, fmt.Errorf("plaid: decrypt access_token: %w", err)
	}
	defer zeroBytes(tokenBytes)

	snapshot, err := s.client.FetchHoldings(ctx, string(tokenBytes))
	if err != nil {
		return HoldingsReconcileResult{}, err
	}

	// Resolve securities to assets outside the DB transaction.
	assetBySecurity := map[string]int64{}
	assetCurrency := map[string]string{}
	for _, sec := range snapshot.Securities {
		symbol := SecuritySymbol(sec)
		if symbol == "" {
			continue
		}
		kind := SecurityKind(sec.Type, sec.IsCashEquivalent)
		display := sec.Name
		if display == "" {
			display = symbol
		}
		asset, err := s.assetRepo.EnsureBySymbolKind(ctx, symbol, kind, display)
		if err != nil {
			return HoldingsReconcileResult{}, fmt.Errorf("plaid: ensure asset %s: %w", symbol, err)
		}
		assetBySecurity[sec.PlaidSecurityID] = asset.ID
		assetCurrency[sec.PlaidSecurityID] = sec.IsoCurrencyCode
	}

	result := HoldingsReconcileResult{Holdings: len(snapshot.Holdings)}
	accountIDCache := map[string]accountRef{}

	now := time.Now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		acctRepo := repository.NewAccountRepository(tx)
		posRepo := repository.NewPositionRepository(tx)
		priceRepo := repository.NewPriceRepository(tx)
		txRepo := repository.NewTransactionRepository(tx)
		obsRepo := repository.NewBalanceObservationRepository(tx)

		for _, h := range snapshot.Holdings {
			ref, err := resolveAccount(ctx, acctRepo, userID, h.PlaidAccountID, accountIDCache)
			if err != nil {
				// No local account — caller should have run sync-accounts.
				return fmt.Errorf("plaid: holdings: %w", err)
			}
			assetID, ok := assetBySecurity[h.PlaidSecurityID]
			if !ok {
				continue
			}

			// Reconcile the holding quantity into the ledger (ADR-0017):
			// Plaid's snapshot is the reported truth, and ReconcilePosition
			// writes an opening_balance anchor (first encounter / no trade
			// legs) or a typed adjustment (drift from trade-derived fold) for
			// the delta — never a synthetic *trade*. After it returns the fold
			// equals h.Quantity, so the position upsert below keeps
			// positions.quantity == Σ transactions.amount for this asset.
			existing, err := findLivePosition(ctx, tx, userID, ref.ID, assetID)
			if err != nil {
				return err
			}
			rec, err := service.ReconcilePosition(ctx, txRepo, obsRepo,
				userID, ref.ID, assetID, h.Quantity, now, "plaid")
			if err != nil {
				return fmt.Errorf("plaid: reconcile holding (acct=%d asset=%d): %w", ref.ID, assetID, err)
			}
			if rec != nil {
				log.Printf("plaid: holdings reconcile drift (acct=%d asset=%d plaid=%s) — wrote %s of %s",
					ref.ID, assetID, h.Quantity.String(), rec.Kind, rec.Amount.String())
				result.PositionsAdjusted++
			}
			pos := &model.Position{
				UserID:    userID,
				AccountID: ref.ID,
				AssetID:   assetID,
				Quantity:  h.Quantity,
			}
			if !h.CostBasis.IsZero() {
				cb := h.CostBasis
				pos.CostBasis = &cb
			} else if existing != nil && existing.CostBasis != nil {
				cb := *existing.CostBasis
				pos.CostBasis = &cb
			}
			if err := posRepo.Upsert(ctx, pos); err != nil {
				return fmt.Errorf("upsert position: %w", err)
			}

			// Persist a Plaid-fed price observation when present. Use
			// the holding's currency (security's IsoCurrencyCode) as
			// the quote asset; resolves via the account's fiat asset
			// when matching.
			if !h.InstitutionPrice.IsZero() {
				currency := assetCurrency[h.PlaidSecurityID]
				if currency == "" {
					currency = "USD"
				}
				quoteAsset, err := s.assetRepo.EnsureBySymbolKind(ctx, currency, model.AssetKindFiat, currency)
				if err != nil {
					return fmt.Errorf("plaid: ensure quote asset %s: %w", currency, err)
				}
				if err := priceRepo.Insert(ctx, &model.Price{
					AssetID:      assetID,
					QuoteAssetID: quoteAsset.ID,
					AsOf:         time.Now().UTC(),
					Price:        h.InstitutionPrice,
					Source:       "plaid",
				}); err != nil {
					return fmt.Errorf("insert price: %w", err)
				}
				result.PricesObserved++
			}
		}
		return nil
	})
	if err != nil {
		return HoldingsReconcileResult{}, err
	}
	return result, nil
}

// findLivePosition returns the position row for (user, account, asset)
// or nil when none exists. Used by reconciliation to detect first-
// encounter holdings (no existing row → no drift to log).
func findLivePosition(ctx context.Context, tx *gorm.DB, userID, accountID, assetID int64) (*model.Position, error) {
	var p model.Position
	err := tx.WithContext(ctx).
		Where("user_id = ? AND account_id = ? AND asset_id = ?", userID, accountID, assetID).
		First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}
