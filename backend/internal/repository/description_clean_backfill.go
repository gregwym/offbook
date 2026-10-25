package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/merchant"
)

// descriptionCleanBatch bounds one backfill transaction.
const descriptionCleanBatch = 500

// BackfillDescriptionClean writes description_clean for flow rows imported
// before #588 that have a bank description but neither a merchant name nor
// a cleaned description, using the same merchant.CleanDescription the
// insert hook applies. Labels only: amounts, description, merchant_name and
// every other column stay untouched. Idempotent — a row it fills no longer
// matches, so a second run updates nothing. A system maintenance pass over
// all users' rows; it reads and writes each row's own text only.
func BackfillDescriptionClean(ctx context.Context, db *gorm.DB) (int64, error) {
	type row struct {
		ID          int64
		Description string
	}
	var total int64
	afterID := int64(0)
	for {
		var rows []row
		if err := db.WithContext(ctx).Raw(`
			SELECT id, description FROM transactions
			WHERE id > ? AND kind = 'flow' AND description_clean IS NULL
			  AND COALESCE(TRIM(merchant_name), '') = ''
			  AND COALESCE(TRIM(description), '') <> ''
			ORDER BY id LIMIT ?`, afterID, descriptionCleanBatch).Scan(&rows).Error; err != nil {
			return total, fmt.Errorf("backfill description_clean: scan: %w", err)
		}
		if len(rows) == 0 {
			return total, nil
		}
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, r := range rows {
				res := tx.Exec(`UPDATE transactions SET description_clean = ? WHERE id = ? AND description_clean IS NULL`,
					merchant.CleanDescription(r.Description), r.ID)
				if res.Error != nil {
					return res.Error
				}
				total += res.RowsAffected
			}
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("backfill description_clean: update: %w", err)
		}
		afterID = rows[len(rows)-1].ID
	}
}
