BEGIN;

DROP INDEX IF EXISTS ix_ingestion_jobs_account_created;
DROP INDEX IF EXISTS ix_ingestion_jobs_account_hash;

ALTER TABLE ingestion_jobs
    DROP COLUMN IF EXISTS reconciled,
    DROP COLUMN IF EXISTS doc_totals,
    DROP COLUMN IF EXISTS row_sum,
    DROP COLUMN IF EXISTS rows_error,
    DROP COLUMN IF EXISTS rows_duplicate,
    DROP COLUMN IF EXISTS content_hash;

COMMIT;
