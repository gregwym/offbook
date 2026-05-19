-- #167: shared threads need per-message authorship.
--
-- ai_messages had no user_id column because M7 shipped before shared
-- threads existed and the thread already implied a single user. With
-- shared_with_household=true threads, multiple users can post to the same
-- thread, so we need to know who said what. Assistant messages set
-- user_id to NULL.
--
-- Nullable on purpose: existing user-role rows from before this change
-- have no recoverable user_id (the writer was always the thread owner,
-- but we don't backfill — leaving NULL signals "pre-attribution").
ALTER TABLE ai_messages
    ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

-- Indexed for the (thread, user) UI grouping use case ("which messages
-- in this shared thread were mine?"). Partial because user_id is rarely
-- queried in isolation.
CREATE INDEX ix_ai_messages_thread_user
    ON ai_messages (thread_id, user_id)
    WHERE user_id IS NOT NULL;
