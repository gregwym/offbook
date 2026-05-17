package service

import (
	"context"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// CategoryService is intentionally thin — categories are shared lookup data
// (seeded in migration 000002) with no per-user ownership. The repository
// already returns canonically-ordered rows.
type CategoryService struct {
	repo repository.CategoryRepository
}

func NewCategoryService(repo repository.CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

func (s *CategoryService) List(ctx context.Context) ([]model.Category, error) {
	return s.repo.List(ctx)
}
