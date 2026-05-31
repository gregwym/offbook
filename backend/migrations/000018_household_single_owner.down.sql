BEGIN;

-- Reverse #283: restore owner_id, backfilled from the active owner member.
DROP INDEX IF EXISTS uq_household_single_owner;

ALTER TABLE households ADD COLUMN owner_id BIGINT REFERENCES users(id);
UPDATE households h
   SET owner_id = (
       SELECT hm.user_id FROM household_members hm
        WHERE hm.household_id = h.id
          AND hm.role = 'owner'
          AND hm.purged_at IS NULL
        LIMIT 1
   );
ALTER TABLE households ALTER COLUMN owner_id SET NOT NULL;

COMMIT;
