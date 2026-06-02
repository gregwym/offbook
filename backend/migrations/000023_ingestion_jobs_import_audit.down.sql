BEGIN;

-- Restore transactions.source to its pre-000023 set (keeping 'system' from 000021).
ALTER TABLE transactions DROP CONSTRAINT transactions_source_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_source_check
    CHECK (source IN ('plaid', 'csv', 'pdf', 'manual', 'system'));

DROP INDEX IF EXISTS ix_ingestion_jobs_user_created;

ALTER TABLE ingestion_jobs DROP CONSTRAINT ingestion_jobs_status_check;
ALTER TABLE ingestion_jobs ADD CONSTRAINT ingestion_jobs_status_check
    CHECK (status IN ('pending', 'processing', 'completed', 'failed'));

ALTER TABLE ingestion_jobs DROP CONSTRAINT ingestion_jobs_source_check;
ALTER TABLE ingestion_jobs ADD CONSTRAINT ingestion_jobs_source_check
    CHECK (source IN ('csv', 'pdf'));

ALTER TABLE ingestion_jobs DROP CONSTRAINT IF EXISTS ingestion_jobs_extractor_check;

ALTER TABLE ingestion_jobs
    DROP COLUMN extraction,
    DROP COLUMN consented_at,
    DROP COLUMN extractor,
    DROP COLUMN provider,
    DROP COLUMN user_id;

COMMIT;
