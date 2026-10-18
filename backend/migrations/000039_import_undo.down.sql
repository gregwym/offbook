ALTER TABLE ingestion_jobs
    DROP COLUMN IF EXISTS rows_undone,
    DROP COLUMN IF EXISTS undone_at;

DROP INDEX IF EXISTS ix_transactions_import_job;

ALTER TABLE transactions DROP COLUMN IF EXISTS import_job_id;
