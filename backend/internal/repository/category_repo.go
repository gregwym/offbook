package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
)

// CategoryRepository reads the system taxonomy (user_id NULL, migration
// 000002) plus user-owned categories (#573). Every read is scoped: a caller
// sees system rows and its own, never another user's.
type CategoryRepository interface {
	// GetVisible fetches one category the owner may reference: a system row,
	// or one owned by *userID. A nil userID (a household-owned plan) sees
	// system rows only. Another user's category is ErrNotFound.
	GetVisible(ctx context.Context, userID *int64, id int64) (*model.Category, error)
	// List returns the system taxonomy (user_id NULL) plus the caller's
	// own categories.
	List(ctx context.Context, userID int64) ([]model.Category, error)
	// NameTaken reports whether a live category the user can see already
	// has this name (case-insensitive), ignoring exceptID when non-zero.
	NameTaken(ctx context.Context, userID int64, name string, exceptID int64) (bool, error)
	// SlugTaken reports whether the user already owns a live category with
	// this slug (the uq_categories_slug namespace).
	SlugTaken(ctx context.Context, userID int64, slug string) (bool, error)
	Create(ctx context.Context, c *model.Category) error
	// Rename sets the name of a category the user owns. ErrNotFound when
	// the row is missing, a system row, or another user's.
	Rename(ctx context.Context, userID, id int64, name string) error
	// Usage counts what references the user's category id (#575).
	Usage(ctx context.Context, userID, id int64) (CategoryUsage, error)
	// Reassign moves everything that references the user's category from
	// onto to (nil = Uncategorized), then soft-deletes from (#575). Callers
	// run it inside a db.Transaction; see CategoryService.Delete.
	Reassign(ctx context.Context, userID, from int64, to *model.Category) (CategoryUsage, error)
}

// CategoryUsage is what references a category: live transactions and split
// lines, live rules and budgets, and the user's own subcategories.
type CategoryUsage struct {
	Transactions  int64 `json:"transactions"`
	Splits        int64 `json:"splits"`
	Rules         int64 `json:"rules"`
	Budgets       int64 `json:"budgets"`
	Subcategories int64 `json:"subcategories"`
}

type categoryRepo struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepo{db: db}
}

func (r *categoryRepo) GetVisible(ctx context.Context, userID *int64, id int64) (*model.Category, error) {
	q := r.db.WithContext(ctx)
	if userID == nil {
		q = q.Where("user_id IS NULL")
	} else {
		q = q.Where("user_id IS NULL OR user_id = ?", *userID)
	}
	var c model.Category
	if err := q.First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *categoryRepo) List(ctx context.Context, userID int64) ([]model.Category, error) {
	var out []model.Category
	if err := r.db.WithContext(ctx).
		Where("user_id IS NULL OR user_id = ?", userID).
		Order("name ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *categoryRepo) NameTaken(ctx context.Context, userID int64, name string, exceptID int64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Category{}).
		Where("(user_id IS NULL OR user_id = ?) AND lower(name) = ? AND id <> ?",
			userID, strings.ToLower(name), exceptID).
		Count(&n).Error
	return n > 0, err
}

func (r *categoryRepo) SlugTaken(ctx context.Context, userID int64, slug string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Category{}).
		Where("user_id = ? AND slug = ?", userID, slug).
		Count(&n).Error
	return n > 0, err
}

func (r *categoryRepo) Create(ctx context.Context, c *model.Category) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *categoryRepo) Rename(ctx context.Context, userID, id int64, name string) error {
	res := r.db.WithContext(ctx).Model(&model.Category{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("name", name)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *categoryRepo) Usage(ctx context.Context, userID, id int64) (CategoryUsage, error) {
	var u CategoryUsage
	err := r.db.WithContext(ctx).Raw(`
		SELECT
		  (SELECT count(*) FROM transactions
		    WHERE user_id = @u AND category_id = @c AND deleted_at IS NULL) AS transactions,
		  (SELECT count(*) FROM transaction_splits s JOIN transactions t ON t.id = s.transaction_id
		    WHERE s.user_id = @u AND s.category_id = @c AND t.deleted_at IS NULL) AS splits,
		  (SELECT count(*) FROM categorization_rules
		    WHERE user_id = @u AND category_id = @c AND deleted_at IS NULL) AS rules,
		  (SELECT count(*) FROM budgets
		    WHERE user_id = @u AND category_id = @c AND deleted_at IS NULL) AS budgets,
		  (SELECT count(*) FROM categories
		    WHERE user_id = @u AND parent_id = @c AND deleted_at IS NULL) AS subcategories`,
		map[string]any{"u": userID, "c": id}).Scan(&u).Error
	return u, err
}

func (r *categoryRepo) Reassign(ctx context.Context, userID, from int64, to *model.Category) (CategoryUsage, error) {
	db := r.db.WithContext(ctx)
	usage, err := r.Usage(ctx, userID, from)
	if err != nil {
		return usage, err
	}
	var toID *int64
	if to != nil {
		toID = &to.ID
	}
	args := map[string]any{"u": userID, "from": from, "to": toID}
	steps := []string{
		// Transactions, soft-deleted ones included so a restore can't point
		// at a deleted category. Moving to another category keeps each row's
		// categorization_method and rule: the rule moves with it below.
		// Moving to Uncategorized clears the provenance with the category.
		`UPDATE transactions SET category_id = @to, updated_at = now(),
		   categorization_method = CASE WHEN CAST(@to AS bigint) IS NULL THEN NULL ELSE categorization_method END,
		   categorization_rule_id = CASE WHEN CAST(@to AS bigint) IS NULL THEN NULL ELSE categorization_rule_id END
		 WHERE user_id = @u AND category_id = @from`,
		`UPDATE transaction_splits SET category_id = @to WHERE user_id = @u AND category_id = @from`,
		// The rules-run Undo log restores prev_category_id; keep it pointing
		// at a live category.
		`UPDATE rule_apply_changes c SET prev_category_id = @to
		   FROM rule_apply_runs r
		  WHERE r.id = c.run_id AND r.user_id = @u AND c.prev_category_id = @from`,
		// A rule whose only action was this category has nothing left to do
		// once it goes to Uncategorized: turn it off rather than delete it.
		`UPDATE categorization_rules SET category_id = @to, updated_at = now(),
		   is_active = is_active AND (CAST(@to AS bigint) IS NOT NULL
		     OR coalesce(rename_merchant, '') <> '' OR hide_matches OR mark_reviewed
		     OR cardinality(add_tags) > 0)
		 WHERE user_id = @u AND category_id = @from`,
	}
	if to == nil {
		// The verdict cache can't hold "uncategorized"; budgets can't track it.
		steps = append(steps,
			`DELETE FROM ai_categorization_verdicts WHERE user_id = @u AND category_id = @from`,
			`UPDATE budgets SET deleted_at = now(), updated_at = now()
			  WHERE user_id = @u AND category_id = @from AND deleted_at IS NULL`)
	} else {
		steps = append(steps,
			`UPDATE ai_categorization_verdicts SET category_id = @to, updated_at = now()
			  WHERE user_id = @u AND category_id = @from`,
			// An active budget whose period the target already budgets adds
			// its amount to the target's and is retired; the rest move over.
			`UPDATE budgets b SET amount = b.amount + a.amount, updated_at = now()
			   FROM budgets a
			  WHERE a.user_id = @u AND a.category_id = @from AND a.deleted_at IS NULL AND a.is_active
			    AND b.user_id = @u AND b.category_id = @to AND b.deleted_at IS NULL AND b.is_active
			    AND b.period = a.period`,
			`UPDATE budgets a SET deleted_at = now(), updated_at = now()
			  WHERE a.user_id = @u AND a.category_id = @from AND a.deleted_at IS NULL AND a.is_active
			    AND EXISTS (SELECT 1 FROM budgets b
			                 WHERE b.user_id = @u AND b.category_id = @to AND b.deleted_at IS NULL
			                   AND b.is_active AND b.period = a.period)`,
			`UPDATE budgets SET category_id = @to, updated_at = now()
			  WHERE user_id = @u AND category_id = @from`)
	}
	// Subcategories go under the target when it can be a group (top-level,
	// or about to be: a subcategory of from); otherwise they become
	// top-level. The target itself never becomes its own child.
	targetTop := to != nil && (to.ParentID == nil || *to.ParentID == from)
	args["top"] = targetTop
	steps = append(steps,
		`UPDATE categories SET updated_at = now(),
		   parent_id = CASE WHEN @top AND id <> CAST(@to AS bigint) THEN CAST(@to AS bigint) ELSE NULL END
		 WHERE user_id = @u AND parent_id = @from AND deleted_at IS NULL`,
		`UPDATE categories SET deleted_at = now(), updated_at = now()
		  WHERE user_id = @u AND id = @from AND deleted_at IS NULL`)
	for _, q := range steps {
		if err := db.Exec(q, args).Error; err != nil {
			return usage, err
		}
	}
	return usage, nil
}
