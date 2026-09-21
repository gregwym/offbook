package service

import (
	"context"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// AICategorizationVerdictService is intentionally thin — it exists only so
// the handler never touches the repository directly (go-backend.md). The
// cache is populated exclusively by RunCategorizationPass; this service is
// read-only, the source for the frontend's "promote to rule" affordance
// (ADR-0022 §6).
type AICategorizationVerdictService struct {
	repo repository.AICategorizationVerdictRepository
}

func NewAICategorizationVerdictService(repo repository.AICategorizationVerdictRepository) *AICategorizationVerdictService {
	return &AICategorizationVerdictService{repo: repo}
}

func (s *AICategorizationVerdictService) List(ctx context.Context, userID int64) ([]model.AICategorizationVerdict, error) {
	return s.repo.ListByUser(ctx, userID)
}
