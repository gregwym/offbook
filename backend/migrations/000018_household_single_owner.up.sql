BEGIN;

-- #283: households.owner_id duplicated household_members.role='owner'.
--
-- role is the RBAC backbone (owner | contributor | view_only) and the only
-- ownership signal auth actually reads (the LAST_OWNER guard counts
-- role='owner' rows). owner_id was write-only and forced keeping two sources
-- in sync on every transfer. Drop it; make role='owner' the single source of
-- truth, with a partial unique index guaranteeing exactly one active owner
-- per household.
ALTER TABLE households DROP COLUMN IF EXISTS owner_id;

CREATE UNIQUE INDEX uq_household_single_owner
    ON household_members (household_id)
    WHERE role = 'owner' AND purged_at IS NULL;

COMMIT;
