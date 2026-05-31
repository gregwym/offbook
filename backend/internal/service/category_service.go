package service

import (
	"context"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// CategoryService is intentionally thin. Categories are the seeded system
// taxonomy (migration 000002, user_id NULL) plus any user-owned categories
// once CRUD ships (#285). List returns system rows plus the caller's own.
type CategoryService struct {
	repo repository.CategoryRepository
}

func NewCategoryService(repo repository.CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

func (s *CategoryService) List(ctx context.Context, userID int64) ([]model.Category, error) {
	return s.repo.List(ctx, userID)
}
