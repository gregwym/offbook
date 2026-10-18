-- #483: an import's statement closing-balance observation is linked to the
-- import job so Undo can remove it with the adjustment it caused. Pure
-- additive expand; legacy observations keep NULL.
ALTER TABLE account_balance_observations
    ADD COLUMN import_job_id BIGINT REFERENCES ingestion_jobs(id);

CREATE INDEX ix_balance_observations_import_job
    ON account_balance_observations (import_job_id)
    WHERE import_job_id IS NOT NULL;
