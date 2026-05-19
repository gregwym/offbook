DROP INDEX IF EXISTS ix_ai_messages_thread_user;
ALTER TABLE ai_messages DROP COLUMN IF EXISTS user_id;
