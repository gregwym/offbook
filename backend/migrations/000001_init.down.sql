BEGIN;

DROP TABLE IF EXISTS ingestion_jobs;
DROP TABLE IF EXISTS pii_store;
DROP TABLE IF EXISTS ai_messages;
DROP TABLE IF EXISTS ai_threads;
DROP TABLE IF EXISTS shared_goals;
DROP TABLE IF EXISTS shared_budgets;
DROP TABLE IF EXISTS investments;
DROP TABLE IF EXISTS savings_goals;
DROP TABLE IF EXISTS budgets;
DROP TABLE IF EXISTS categorization_rules;
DROP TABLE IF EXISTS account_shares;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS household_invites;
DROP TABLE IF EXISTS household_members;
DROP TABLE IF EXISTS households;
DROP TABLE IF EXISTS instance_config;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;

COMMIT;
