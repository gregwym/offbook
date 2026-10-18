DROP INDEX IF EXISTS ix_balance_observations_import_job;
ALTER TABLE account_balance_observations DROP COLUMN IF EXISTS import_job_id;
