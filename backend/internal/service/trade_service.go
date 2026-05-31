package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// Domain errors for the trade entry surface.
var (
	ErrInvalidTradeKind    = errors.New("trade kind must be 'buy' or 'sell'")
	ErrInvalidQuantity     = errors.New("quantity must be > 0")
	ErrInvalidPrice        = errors.New("price must be > 0")
	ErrUnsupportedAccount  = errors.New("trades may only be recorded on brokerage-type accounts")
	ErrUnknownAsset        = errors.New("asset not found")
	ErrTradeFXUnavailable  = errors.New("no FX rate available for trade date — set the rate in prices first")
	ErrCashAssetMismatch   = errors.New("cash leg must be in the account's primary quote asset")
	ErrSellExceedsHoldings = errors.New("sell quantity exceeds current holding")
	ErrSecurityEqualsQuote = errors.New("trade asset must differ from the account's cash asset")
)

// Brokerage-style accounts are the only ones that accept trades. The
// list mirrors the position-model intent: bank-style accounts hold a
// single cash position and don't need trade pairing.
var brokerageAccountTypes = map[string]struct{}{
	"investment": {},
	"crypto":     {},
}

// RecordTradeInput is the validated payload for POST /accounts/:id/trades.
// Kind ∈ {"buy", "sell"}. Quantity and Price are always supplied positive;
// the service handles sign and pairing.
//
// Price is denominated in the parent account's primary quote asset
// (i.e. the cash sleeve). Multi-currency trades (security price quoted
// in a non-account currency) are out of scope for the manual form —
// users can record them by entering the cash leg manually.
type RecordTradeInput struct {
	Kind      string          // "buy" | "sell"
	AssetID   int64           // security being traded
	Quantity  decimal.Decimal // always positive; service flips sign per kind
	Price     decimal.Decimal // per-unit, in account's quote currency
	TradeDate time.Time
	Notes     *string
}

// TradeRecord bundles the persisted pair + updated positions so the
// handler can return both in a single response without extra fetches.
type TradeRecord struct {
	SecurityLeg      *model.Transaction `json:"security_leg"`
	CashLeg          *model.Transaction `json:"cash_leg"`
	SecurityPosition *model.Position    `json:"security_position"`
	CashPosition     *model.Position    `json:"cash_position"`
}

// TradeService owns the manual "Record a trade" flow and is the entry
// point for future ingestion sources that want to write paired-row
// trades (e.g. Plaid investment-transactions, CSV imports).
type TradeService struct {
	db        *gorm.DB
	accounts  repository.AccountRepository
	assets    repository.AssetRepository
	txns      repository.TransactionRepository
	positions repository.PositionRepository
	prices    repository.PriceRepository
	users     repository.UserRepository
}

func NewTradeService(
	db *gorm.DB,
	accounts repository.AccountRepository,
	assets repository.AssetRepository,
	txns repository.TransactionRepository,
	positions repository.PositionRepository,
	prices repository.PriceRepository,
	users repository.UserRepository,
) *TradeService {
	return &TradeService{
		db:        db,
		accounts:  accounts,
		assets:    assets,
		txns:      txns,
		positions: positions,
		prices:    prices,
		users:     users,
	}
}

// Record validates the input, writes the paired transaction rows in one
// DB transaction, updates the security + cash positions, and recomputes
// cost basis on the security position from the full trade history. The
// invariant from ADR-0013 — "the app never invents transactions" — still
// holds: this method is itself a real event (the user typed it).
func (s *TradeService) Record(ctx context.Context, userID, accountID int64, in RecordTradeInput) (*TradeRecord, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	switch kind {
	case "buy", "sell":
	default:
		return nil, ErrInvalidTradeKind
	}
	if !in.Quantity.IsPositive() {
		return nil, ErrInvalidQuantity
	}
	if !in.Price.IsPositive() {
		return nil, ErrInvalidPrice
	}
	if in.TradeDate.IsZero() {
		return nil, ErrMissingDate
	}

	acct, err := s.accounts.GetByID(ctx, userID, accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("trade: load account: %w", err)
	}
	if _, ok := brokerageAccountTypes[acct.AccountType]; !ok {
		return nil, ErrUnsupportedAccount
	}
	if in.AssetID == acct.PrimaryQuoteAssetID {
		return nil, ErrSecurityEqualsQuote
	}
	if _, err := s.assets.GetByID(ctx, in.AssetID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUnknownAsset
		}
		return nil, fmt.Errorf("trade: load asset: %w", err)
	}

	// Derive signed amounts. Sign convention matches the rest of the app:
	// positive = inflow into the account, negative = outflow. For a buy,
	// the security flows IN, cash flows OUT. For a sell, opposite.
	cashTotal := in.Price.Mul(in.Quantity)
	var secAmount, cashAmount decimal.Decimal
	switch kind {
	case "buy":
		secAmount = in.Quantity
		cashAmount = cashTotal.Neg()
	case "sell":
		secAmount = in.Quantity.Neg()
		cashAmount = cashTotal
	}

	// Pre-check: a sell can't exceed the current holding. Read the
	// security position first (cheap; one indexed row).
	if kind == "sell" {
		positions, err := s.positions.ListByAccountID(ctx, userID, accountID)
		if err != nil {
			return nil, fmt.Errorf("trade: load positions: %w", err)
		}
		held := decimal.Zero
		for _, p := range positions {
			if p.AssetID == in.AssetID {
				held = p.Quantity
				break
			}
		}
		if in.Quantity.GreaterThan(held) {
			return nil, ErrSellExceedsHoldings
		}
	}

	descSec := tradeDescription(kind, in.Quantity, in.Price)
	descCash := descSec // mirror on both legs so the transaction list reads
	method := "manual"
	source := "manual"

	secLeg := &model.Transaction{
		UserID:               userID,
		AccountID:            accountID,
		AssetID:              in.AssetID,
		Kind:                 model.KindTradeLeg,
		Amount:               secAmount,
		Description:          &descSec,
		TransactionDate:      in.TradeDate,
		Source:               source,
		CategorizationMethod: &method,
		Notes:                in.Notes,
	}
	cashLeg := &model.Transaction{
		UserID:               userID,
		AccountID:            accountID,
		AssetID:              acct.PrimaryQuoteAssetID,
		Kind:                 model.KindTradeLeg,
		Amount:               cashAmount,
		Description:          &descCash,
		TransactionDate:      in.TradeDate,
		Source:               source,
		CategorizationMethod: &method,
		Notes:                in.Notes,
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("trade: load user: %w", err)
	}

	var record TradeRecord
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		posRepo := repository.NewPositionRepository(tx)

		if err := txRepo.CreateTradePair(ctx, secLeg, cashLeg); err != nil {
			return fmt.Errorf("create trade pair: %w", err)
		}

		// Recompute the security position from history (quantity + cost
		// basis). The recompute is canonical; we don't trust deltas.
		secResult, err := valuation.Recompute(ctx, txRepo, s.prices, userID, accountID, in.AssetID, user.PrimaryCurrencyAssetID)
		if err != nil {
			if errors.Is(err, valuation.ErrFXUnavailable) {
				return ErrTradeFXUnavailable
			}
			return fmt.Errorf("recompute security: %w", err)
		}
		secPos := &model.Position{
			UserID:    userID,
			AccountID: accountID,
			AssetID:   in.AssetID,
			Quantity:  secResult.Quantity,
		}
		if secResult.HasCostBasis {
			cb := secResult.CostBasis
			secPos.CostBasis = &cb
		}
		if err := posRepo.Upsert(ctx, secPos); err != nil {
			return fmt.Errorf("upsert security position: %w", err)
		}

		// Cash position: quantity-only recompute (cost basis on the
		// primary cash sleeve is meaningless — see Recompute fast path).
		cashResult, err := valuation.Recompute(ctx, txRepo, s.prices, userID, accountID, acct.PrimaryQuoteAssetID, user.PrimaryCurrencyAssetID)
		if err != nil {
			return fmt.Errorf("recompute cash: %w", err)
		}
		cashPos := &model.Position{
			UserID:    userID,
			AccountID: accountID,
			AssetID:   acct.PrimaryQuoteAssetID,
			Quantity:  cashResult.Quantity,
		}
		if err := posRepo.Upsert(ctx, cashPos); err != nil {
			return fmt.Errorf("upsert cash position: %w", err)
		}

		record = TradeRecord{
			SecurityLeg:      secLeg,
			CashLeg:          cashLeg,
			SecurityPosition: secPos,
			CashPosition:     cashPos,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// tradeDescription renders the canonical "Bought N AAPL @ $P" / "Sold
// …" string used on both legs. The transaction list collapses the pair
// using the matching description, so keep this stable.
func tradeDescription(kind string, qty, price decimal.Decimal) string {
	verb := "Bought"
	if kind == "sell" {
		verb = "Sold"
	}
	return fmt.Sprintf("%s %s @ %s", verb, qty.String(), price.String())
}
