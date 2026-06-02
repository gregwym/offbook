BEGIN;

-- ADR-0019 phase 2 wires ingestion_jobs for the first time as both the
-- per-import audit trail and the AI staging store. The table has had no
-- writers since 000001, so it is empty — adding NOT NULL columns is safe
-- without a backfill.
--
--   user_id      — tenancy; every import belongs to the session user.
--   provider     — which AI provider performed extraction ('claude'); NULL for
--                  deterministic CSV.
--   extractor    — 'deterministic' (CSV) | 'ai' (PDF/photo + CSV fallback).
--   consented_at — when the user consented to the cloud egress; NULL when no
--                  egress occurred (deterministic, or a local provider).
--   extraction   — staged rows + detected totals + CSV echo, applied verbatim
--                  on commit so the AI never re-runs (see ADR-0019 §7).
ALTER TABLE ingestion_jobs
    ADD COLUMN user_id      BIGINT NOT NULL REFERENCES users(id),
    ADD COLUMN provider     TEXT,
    ADD COLUMN extractor    TEXT,
    ADD COLUMN consented_at TIMESTAMPTZ,
    ADD COLUMN extraction    JSONB;

ALTER TABLE ingestion_jobs
    ADD CONSTRAINT ingestion_jobs_extractor_check
        CHECK (extractor IS NULL OR extractor IN ('deterministic', 'ai'));

-- Widen the source enum for photo imports.
ALTER TABLE ingestion_jobs DROP CONSTRAINT ingestion_jobs_source_check;
ALTER TABLE ingestion_jobs ADD CONSTRAINT ingestion_jobs_source_check
    CHECK (source IN ('csv', 'pdf', 'photo'));

-- 'extracted' = AI rows staged, awaiting the user's commit (see ADR-0019 §7).
ALTER TABLE ingestion_jobs DROP CONSTRAINT ingestion_jobs_status_check;
ALTER TABLE ingestion_jobs ADD CONSTRAINT ingestion_jobs_status_check
    CHECK (status IN ('pending', 'processing', 'extracted', 'completed', 'failed'));

CREATE INDEX ix_ingestion_jobs_user_created
    ON ingestion_jobs (user_id, created_at DESC);

-- transactions.source gains 'photo' so AI-extracted photo rows are admissible.
-- Preserve 'system' (added in 000021) — do not drop it.
ALTER TABLE transactions DROP CONSTRAINT transactions_source_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_source_check
    CHECK (source IN ('plaid', 'csv', 'pdf', 'photo', 'manual', 'system'));

COMMIT;
