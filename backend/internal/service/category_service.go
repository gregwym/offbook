package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// MaxCategoryName caps a user category's name, in characters.
const MaxCategoryName = 60

var (
	ErrInvalidCategoryName = errors.New("category name must be 1-60 characters")
	ErrCategoryNameTaken   = errors.New("a category with this name already exists")
	ErrInvalidParent       = errors.New("parent must be an existing top-level category")
	ErrCategoryReadOnly    = errors.New("built-in categories can't be renamed or deleted")
	ErrCategoryNotFound    = errors.New("category not found")
	// ErrInvalidReplacement: the replacement for a deleted category must be
	// another category the user can see (#575).
	ErrInvalidReplacement = errors.New("pick another existing category, or Uncategorized")
)

// CategoryService serves the seeded system taxonomy (migration 000002,
// user_id NULL) plus each user's own categories (#573). A user sees and may
// reference system rows and their own, never another user's.
type CategoryService struct {
	repo repository.CategoryRepository
	db   *gorm.DB
}

// WithDB enables Delete, which rewrites several tables in one transaction.
func (s *CategoryService) WithDB(db *gorm.DB) *CategoryService {
	s.db = db
	return s
}

func NewCategoryService(repo repository.CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

func (s *CategoryService) List(ctx context.Context, userID int64) ([]model.Category, error) {
	return s.repo.List(ctx, userID)
}

// CreateCategoryInput is a new user category. ParentID, when set, must be a
// visible top-level category, so the taxonomy stays two levels deep.
type CreateCategoryInput struct {
	Name     string
	ParentID *int64
}

// Create adds a category owned by userID. Names are unique
// (case-insensitive) across everything the user can see, so a picker never
// shows two "Coffee" entries.
func (s *CategoryService) Create(ctx context.Context, userID int64, in CreateCategoryInput) (*model.Category, error) {
	name, err := s.checkName(ctx, userID, in.Name, 0)
	if err != nil {
		return nil, err
	}
	if in.ParentID != nil {
		p, err := s.repo.GetVisible(ctx, &userID, *in.ParentID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, ErrInvalidParent
			}
			return nil, err
		}
		if p.ParentID != nil {
			return nil, ErrInvalidParent
		}
	}
	slug, err := s.freeSlug(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	c := &model.Category{UserID: &userID, ParentID: in.ParentID, Name: name, Slug: slug}
	if err := s.repo.Create(ctx, c); err != nil {
		// A concurrent create of the same name lost the slug race.
		if isUniqueViolation(err) {
			return nil, ErrCategoryNameTaken
		}
		return nil, fmt.Errorf("create category: %w", err)
	}
	return c, nil
}

// Rename changes the name of the user's own category. The slug stays put:
// it is the category's stable key, not a display label.
func (s *CategoryService) Rename(ctx context.Context, userID, id int64, name string) (*model.Category, error) {
	c, err := s.repo.GetVisible(ctx, &userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}
	if c.UserID == nil {
		return nil, ErrCategoryReadOnly
	}
	clean, err := s.checkName(ctx, userID, name, id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Rename(ctx, userID, id, clean); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, fmt.Errorf("rename category: %w", err)
	}
	c.Name = clean
	return c, nil
}

// ownCategory fetches a category the user owns: ErrCategoryNotFound when it
// is missing or another user's, ErrCategoryReadOnly for a built-in one.
func (s *CategoryService) ownCategory(ctx context.Context, repo repository.CategoryRepository, userID, id int64) (*model.Category, error) {
	c, err := repo.GetVisible(ctx, &userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}
	if c.UserID == nil {
		return nil, ErrCategoryReadOnly
	}
	return c, nil
}

// Usage reports what references the user's own category, for the delete
// and merge confirmation (#575).
func (s *CategoryService) Usage(ctx context.Context, userID, id int64) (repository.CategoryUsage, error) {
	if _, err := s.ownCategory(ctx, s.repo, userID, id); err != nil {
		return repository.CategoryUsage{}, err
	}
	return s.repo.Usage(ctx, userID, id)
}

// Delete removes the user's own category, moving its transactions, split
// lines, rules, budgets and subcategories to replaceWith (nil =
// Uncategorized) first, all in one transaction (#575). Deleting with a
// replacement is how two categories are merged. It returns what was moved.
func (s *CategoryService) Delete(ctx context.Context, userID, id int64, replaceWith *int64) (repository.CategoryUsage, error) {
	var moved repository.CategoryUsage
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := repository.NewCategoryRepository(tx)
		// Lock the row so a concurrent delete or merge of the same category
		// waits, then re-reads it as deleted.
		if err := tx.Exec("SELECT 1 FROM categories WHERE id = ? AND user_id = ? FOR UPDATE", id, userID).Error; err != nil {
			return err
		}
		if _, err := s.ownCategory(ctx, repo, userID, id); err != nil {
			return err
		}
		var to *model.Category
		if replaceWith != nil {
			if *replaceWith == id {
				return ErrInvalidReplacement
			}
			c, err := repo.GetVisible(ctx, &userID, *replaceWith)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					return ErrInvalidReplacement
				}
				return err
			}
			to = c
		}
		var err error
		moved, err = repo.Reassign(ctx, userID, id, to)
		return err
	})
	if err != nil {
		var empty repository.CategoryUsage
		if errors.Is(err, ErrCategoryNotFound) || errors.Is(err, ErrCategoryReadOnly) || errors.Is(err, ErrInvalidReplacement) {
			return empty, err
		}
		return empty, fmt.Errorf("delete category: %w", err)
	}
	return moved, nil
}

func (s *CategoryService) checkName(ctx context.Context, userID int64, name string, exceptID int64) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > MaxCategoryName {
		return "", ErrInvalidCategoryName
	}
	taken, err := s.repo.NameTaken(ctx, userID, name, exceptID)
	if err != nil {
		return "", err
	}
	if taken {
		return "", ErrCategoryNameTaken
	}
	return name, nil
}

// freeSlug derives a slug from the name that is free in the user's
// namespace, suffixing -2, -3, … when two names slug the same ("Kids!" and
// "Kids?").
func (s *CategoryService) freeSlug(ctx context.Context, userID int64, name string) (string, error) {
	base := slugify(name)
	for i := 1; i <= 50; i++ {
		slug := base
		if i > 1 {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		taken, err := s.repo.SlugTaken(ctx, userID, slug)
		if err != nil {
			return "", err
		}
		if !taken {
			return slug, nil
		}
	}
	return "", ErrCategoryNameTaken
}

// slugify lowercases letters and digits and joins the runs between them
// with "-". A name with none (all emoji) slugs to "category".
func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	if b.Len() == 0 {
		return "category"
	}
	return b.String()
}
