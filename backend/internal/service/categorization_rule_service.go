package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

var (
	ErrRuleNotFound     = errors.New("categorization rule not found")
	ErrEmptyPattern     = errors.New("pattern must not be empty")
	ErrInvalidMatchType = errors.New("match_type must be one of: contains, regex, exact")
	ErrInvalidRegex     = errors.New("pattern is not a valid regular expression")
	ErrInvalidPriority  = errors.New("priority must be >= 0")
	ErrUnknownCategory  = errors.New("category does not exist")
)

var validMatchTypes = map[string]struct{}{
	"contains": {},
	"regex":    {},
	"exact":    {},
}

type CreateRuleInput struct {
	Pattern    string
	MatchType  string
	CategoryID int64
	Priority   int
	IsActive   *bool
}

// UpdateRuleInput is a sparse patch. Nil pointer = leave alone.
type UpdateRuleInput struct {
	Pattern    *string
	MatchType  *string
	CategoryID *int64
	Priority   *int
	IsActive   *bool
}

// CategorizationRuleService owns rule validation and CRUD. It depends on
// CategoryRepository only to verify CategoryID points at a real row — rules
// have no cross-user visibility, so no further dependencies are needed.
type CategorizationRuleService struct {
	repo    repository.CategorizationRuleRepository
	catRepo repository.CategoryRepository
}

func NewCategorizationRuleService(
	repo repository.CategorizationRuleRepository,
	catRepo repository.CategoryRepository,
) *CategorizationRuleService {
	return &CategorizationRuleService{repo: repo, catRepo: catRepo}
}

func (s *CategorizationRuleService) Create(ctx context.Context, userID int64, in CreateRuleInput) (*model.CategorizationRule, error) {
	r := &model.CategorizationRule{
		UserID:     userID,
		Pattern:    strings.TrimSpace(in.Pattern),
		MatchType:  strings.TrimSpace(in.MatchType),
		CategoryID: in.CategoryID,
		Priority:   in.Priority,
		IsActive:   true,
	}
	if in.IsActive != nil {
		r.IsActive = *in.IsActive
	}
	if err := s.validate(ctx, r); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, r); err != nil {
		return nil, fmt.Errorf("create rule: %w", err)
	}
	return r, nil
}

func (s *CategorizationRuleService) Get(ctx context.Context, userID, id int64) (*model.CategorizationRule, error) {
	r, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	return r, nil
}

func (s *CategorizationRuleService) List(ctx context.Context, userID int64) ([]model.CategorizationRule, error) {
	return s.repo.List(ctx, userID)
}

func (s *CategorizationRuleService) Update(ctx context.Context, userID, id int64, in UpdateRuleInput) (*model.CategorizationRule, error) {
	r, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	if in.Pattern != nil {
		r.Pattern = strings.TrimSpace(*in.Pattern)
	}
	if in.MatchType != nil {
		r.MatchType = strings.TrimSpace(*in.MatchType)
	}
	if in.CategoryID != nil {
		r.CategoryID = *in.CategoryID
	}
	if in.Priority != nil {
		r.Priority = *in.Priority
	}
	if in.IsActive != nil {
		r.IsActive = *in.IsActive
	}
	if err := s.validate(ctx, r); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, r); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("update rule: %w", err)
	}
	return r, nil
}

func (s *CategorizationRuleService) SoftDelete(ctx context.Context, userID, id int64) error {
	if err := s.repo.SoftDelete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrRuleNotFound
		}
		return fmt.Errorf("soft delete rule: %w", err)
	}
	return nil
}

func (s *CategorizationRuleService) validate(ctx context.Context, r *model.CategorizationRule) error {
	if r.Pattern == "" {
		return ErrEmptyPattern
	}
	if _, ok := validMatchTypes[r.MatchType]; !ok {
		return ErrInvalidMatchType
	}
	if r.MatchType == "regex" {
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return ErrInvalidRegex
		}
	}
	if r.Priority < 0 {
		return ErrInvalidPriority
	}
	if _, err := s.catRepo.GetByID(ctx, r.CategoryID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrUnknownCategory
		}
		return fmt.Errorf("validate category: %w", err)
	}
	return nil
}
