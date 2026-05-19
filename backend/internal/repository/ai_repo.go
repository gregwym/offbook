package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// AIThreadRepository is the data-access contract for ai_threads.
// Personal-thread reads are scoped by user_id — there is no "fetch by id"
// without an owning user, so a sibling user passing a guessed id gets
// ErrNotFound, not someone else's thread. Shared-thread reads layer on
// top via the household_id + shared_with_household columns.
type AIThreadRepository interface {
	Create(ctx context.Context, t *model.AIThread) error
	GetByID(ctx context.Context, userID, id int64) (*model.AIThread, error)
	ListByUser(ctx context.Context, userID int64) ([]model.AIThread, error)
	UpdateTitle(ctx context.Context, userID, id int64, title string) error
	// GetByIDForMember returns a thread that the user can access:
	// either they own it, or it is `shared_with_household=true` and
	// belongs to the supplied household. Used by the /h/ai routes.
	GetByIDForMember(ctx context.Context, userID, householdID, id int64) (*model.AIThread, error)
	// ListSharedByHousehold returns shared threads bound to a household,
	// newest activity first. Caller has already gated on membership.
	ListSharedByHousehold(ctx context.Context, householdID int64) ([]model.AIThread, error)
}

// AIMessageRepository is the data-access contract for ai_messages.
// Messages are append-only — no soft delete (cascades on thread delete).
// All reads go through a thread id that the service already
// authorization-checked, so this repo doesn't take user_id.
type AIMessageRepository interface {
	Create(ctx context.Context, m *model.AIMessage) error
	ListByThread(ctx context.Context, threadID int64) ([]model.AIMessage, error)
}

type aiThreadRepo struct {
	db *gorm.DB
}

func NewAIThreadRepository(db *gorm.DB) AIThreadRepository {
	return &aiThreadRepo{db: db}
}

func (r *aiThreadRepo) Create(ctx context.Context, t *model.AIThread) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *aiThreadRepo) GetByID(ctx context.Context, userID, id int64) (*model.AIThread, error) {
	var t model.AIThread
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *aiThreadRepo) ListByUser(ctx context.Context, userID int64) ([]model.AIThread, error) {
	var out []model.AIThread
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *aiThreadRepo) GetByIDForMember(ctx context.Context, userID, householdID, id int64) (*model.AIThread, error) {
	var t model.AIThread
	err := r.db.WithContext(ctx).
		Where("id = ?", id).
		Where(
			"(user_id = ?) OR (shared_with_household = TRUE AND household_id = ?)",
			userID, householdID,
		).
		First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *aiThreadRepo) ListSharedByHousehold(ctx context.Context, householdID int64) ([]model.AIThread, error) {
	var out []model.AIThread
	err := r.db.WithContext(ctx).
		Where("shared_with_household = TRUE AND household_id = ?", householdID).
		Order("updated_at DESC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *aiThreadRepo) UpdateTitle(ctx context.Context, userID, id int64, title string) error {
	res := r.db.WithContext(ctx).
		Model(&model.AIThread{}).
		Where("user_id = ? AND id = ?", userID, id).
		Update("title", title)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

type aiMessageRepo struct {
	db *gorm.DB
}

func NewAIMessageRepository(db *gorm.DB) AIMessageRepository {
	return &aiMessageRepo{db: db}
}

func (r *aiMessageRepo) Create(ctx context.Context, m *model.AIMessage) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *aiMessageRepo) ListByThread(ctx context.Context, threadID int64) ([]model.AIMessage, error) {
	var out []model.AIMessage
	if err := r.db.WithContext(ctx).
		Where("thread_id = ?", threadID).
		Order("created_at ASC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
