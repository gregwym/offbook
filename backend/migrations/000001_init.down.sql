BEGIN;

DROP TABLE IF EXISTS ingestion_jobs;
DROP TABLE IF EXISTS pii_store;
DROP TABLE IF EXISTS ai_messages;
DROP TABLE IF EXISTS ai_conversations;
DROP TABLE IF EXISTS investments;
DROP TABLE IF EXISTS savings_goals;
DROP TABLE IF EXISTS budgets;
DROP TABLE IF EXISTS categorization_rules;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS categories;

COMMIT;
