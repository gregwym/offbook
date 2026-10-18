-- IMP-01 / Goal #423: a committed statement import can be undone. Pure
-- additive expand: transactions remember which import created them, and the
-- audit row records when (and how many rows) an undo removed. Legacy rows keep
-- NULL and are therefore not undoable.
ALTER TABLE transactions
    ADD COLUMN import_job_id BIGINT REFERENCES ingestion_jobs(id);

CREATE INDEX ix_transactions_import_job
    ON transactions (import_job_id)
    WHERE import_job_id IS NOT NULL;

ALTER TABLE ingestion_jobs
    ADD COLUMN undone_at TIMESTAMPTZ,
    ADD COLUMN rows_undone INTEGER;
