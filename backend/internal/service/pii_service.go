// pii_service is the ONLY service that receives a PIIRepository.
// See ARCHITECTURE.md "PII Isolation" — the AI layer and all other domain
// services MUST NOT depend on pii_repo or this service.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gregwym/offbook/backend/internal/repository"
)

const (
	piiEntityAccount = "account"
)

// Account PII field allowlist. Mirrors ARCHITECTURE.md's "What goes in pii_store"
// table. Anything outside this set is rejected so callers can't smuggle
// arbitrary blobs into the PII table.
var allowedAccountPIIFields = map[string]struct{}{
	"holder_name":    {},
	"account_number": {},
	"routing_number": {},
	"address":        {},
}

// Domain errors.
var (
	ErrInvalidPIIField = errors.New("invalid pii field")
	ErrEmptyPIIValue   = errors.New("pii value must not be empty")
)

// PIIService is the only place that wires pii_repo. It depends on
// AccountService only to check existence — never to mutate accounts.
type PIIService struct {
	repo    repository.PIIRepository
	accSvc  *AccountService
}

func NewPIIService(repo repository.PIIRepository, accSvc *AccountService) *PIIService {
	return &PIIService{repo: repo, accSvc: accSvc}
}

// GetAccountPII returns all stored PII fields for the account.
// Returns ErrAccountNotFound if the account doesn't exist (or is soft-deleted).
func (s *PIIService) GetAccountPII(ctx context.Context, accountID int64) (map[string]string, error) {
	if _, err := s.accSvc.Get(ctx, accountID); err != nil {
		return nil, err
	}
	return s.repo.GetAll(ctx, piiEntityAccount, accountID)
}

// SetAccountPII upserts the provided fields. Fields not in the map are left
// untouched; pass an empty string value to clear a field's stored value
// (we still write the row — explicit "" beats silent loss).
//
// NOTE: per ADR #21 (backlog) the orphan-cleanup policy for PII on account
// soft-delete is not yet decided. Today, soft-deleting an account leaves
// PII rows behind. Revisit once #21 lands.
func (s *PIIService) SetAccountPII(ctx context.Context, accountID int64, fields map[string]string) error {
	if _, err := s.accSvc.Get(ctx, accountID); err != nil {
		return err
	}
	for field, value := range fields {
		field = strings.TrimSpace(field)
		if _, ok := allowedAccountPIIFields[field]; !ok {
			return fmt.Errorf("%w: %s", ErrInvalidPIIField, field)
		}
		if err := s.repo.Set(ctx, piiEntityAccount, accountID, field, value); err != nil {
			return fmt.Errorf("set pii %s: %w", field, err)
		}
	}
	return nil
}

// AllowedAccountPIIFields returns the canonical field allowlist. Exported for
// handler-side documentation/error messages.
func AllowedAccountPIIFields() []string {
	out := make([]string, 0, len(allowedAccountPIIFields))
	for k := range allowedAccountPIIFields {
		out = append(out, k)
	}
	return out
}
