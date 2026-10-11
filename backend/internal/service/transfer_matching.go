package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// defaultTransferMatchWindowDays bounds how many days apart two opposite-
// amount transactions can fall and still be proposed as the same transfer.
// Same-day ACH/wire transfers are the common case; a few days covers the
// "initiated Friday, posted Monday" lag some institutions have.
const defaultTransferMatchWindowDays = 3

// Domain errors for #371 transfer matching. ErrTransactionNotFound (shared
// with transaction_service.go) covers the "doesn't exist / not yours" case.
var (
	ErrTransferSameAccount    = errors.New("cannot match a transfer within the same account")
	ErrTransferNotFlow        = errors.New("only ordinary (kind=flow) transactions can be matched as a transfer")
	ErrTransferAlreadyLinked  = errors.New("transaction is already linked as a transfer")
	ErrTransferAmountMismatch = errors.New("transfer amounts must be exact opposites")
)

// TransferMatchingService implements #371: a deterministic pass proposing
// is_transfer pairs across a user's own accounts, plus the confirm/unlink
// actions that actually link or unlink them. Nothing is linked silently —
// ProposeMatches is a pure read; only Confirm writes.
type TransferMatchingService struct {
	repo repository.TransactionRepository
}

func NewTransferMatchingService(repo repository.TransactionRepository) *TransferMatchingService {
	return &TransferMatchingService{repo: repo}
}

// ProposeMatches returns the current set of candidate transfer pairs for the
// user. Recomputed fresh on every call — safe to call after any import
// (Plaid sync, CSV, statement) or on demand from the Transactions page.
func (s *TransferMatchingService) ProposeMatches(ctx context.Context, userID int64) ([]repository.TransferCandidate, error) {
	return s.repo.ListTransferCandidates(ctx, userID, defaultTransferMatchWindowDays)
}

// Confirm links two transactions as a matched transfer. It re-validates the
// accounting rules server-side (different accounts, both kind=flow, exact
// opposite amounts, neither already paired) rather than trusting that the
// pair the UI is confirming still looks like the proposal it was shown —
// the data may have changed since ProposeMatches ran.
func (s *TransferMatchingService) Confirm(ctx context.Context, userID, idA, idB int64) (*model.Transaction, *model.Transaction, error) {
	if idA == idB {
		return nil, nil, ErrTransferSameAccount
	}
	a, err := s.getOwned(ctx, userID, idA)
	if err != nil {
		return nil, nil, err
	}
	b, err := s.getOwned(ctx, userID, idB)
	if err != nil {
		return nil, nil, err
	}
	if a.AccountID == b.AccountID {
		return nil, nil, ErrTransferSameAccount
	}
	if a.Kind != model.KindFlow || b.Kind != model.KindFlow {
		return nil, nil, ErrTransferNotFlow
	}
	if a.IsTransfer || b.IsTransfer || a.TransferPairID != nil || b.TransferPairID != nil {
		return nil, nil, ErrTransferAlreadyLinked
	}
	if !a.Amount.Equal(b.Amount.Neg()) {
		return nil, nil, ErrTransferAmountMismatch
	}

	linkedA, linkedB, err := s.repo.LinkTransferPair(ctx, userID, idA, idB)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, ErrTransactionNotFound
		}
		return nil, nil, fmt.Errorf("link transfer pair: %w", err)
	}
	return linkedA, linkedB, nil
}

// Unlink clears is_transfer+transfer_pair_id on transactionID and its
// linked partner, restoring both rows to ordinary flow transactions.
func (s *TransferMatchingService) Unlink(ctx context.Context, userID, transactionID int64) error {
	if err := s.repo.UnlinkTransferPair(ctx, userID, transactionID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTransactionNotFound
		}
		return fmt.Errorf("unlink transfer pair: %w", err)
	}
	return nil
}

func (s *TransferMatchingService) getOwned(ctx context.Context, userID, id int64) (*model.Transaction, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	return t, nil
}
