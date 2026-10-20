-- #513: mark bank transactions that haven't posted yet. Plaid sync sets it
-- from the wire; the pending -> posted adoption clears it on the same row,
-- so user edits carry over. Additive; existing rows are posted (false).
ALTER TABLE transactions ADD COLUMN pending BOOLEAN NOT NULL DEFAULT false;
