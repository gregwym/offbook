BEGIN;

-- #376 import history. Pure additive expand: new nullable columns on the
-- append-only import audit table, no backfill needed (legacy rows keep NULL).
--
--   content_hash   — hex SHA-256 of the uploaded document bytes; powers
--                    duplicate-statement detection per (user, account).
--   rows_duplicate — rows skipped as already-imported at commit time.
--   rows_error     — rows the extractor/parser could not read.
--   row_sum        — signed sum of valid rows (NUMERIC, exact).
--   doc_totals     — statement totals the extractor detected (JSON array of
--                    decimal strings); NULL when none.
--   reconciled     — NULL = no statement total to compare; else whether the
--                    row sum matched one.
ALTER TABLE ingestion_jobs
    ADD COLUMN content_hash   TEXT,
    ADD COLUMN rows_duplicate INTEGER,
    ADD COLUMN rows_error     INTEGER,
    ADD COLUMN row_sum        NUMERIC(30, 18),
    ADD COLUMN doc_totals     JSONB,
    ADD COLUMN reconciled     BOOLEAN;

CREATE INDEX ix_ingestion_jobs_account_hash
    ON ingestion_jobs (user_id, account_id, content_hash)
    WHERE content_hash IS NOT NULL;

CREATE INDEX ix_ingestion_jobs_account_created
    ON ingestion_jobs (user_id, account_id, created_at DESC);

COMMIT;
