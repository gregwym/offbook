ALTER TABLE transactions
    DROP COLUMN IF EXISTS restored_after_user_delete_at,
    DROP COLUMN IF EXISTS deleted_by;
