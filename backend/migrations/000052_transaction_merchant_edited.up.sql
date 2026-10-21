-- Daily review (#569): a user can fix a transaction's merchant name. The fix
-- is pinned by merchant_edited_at so Plaid sync (modified, resurrected,
-- pending -> posted) and rule renames keep it instead of overwriting it.
-- NULL = not user-edited (bank/rule-owned). Additive: one nullable column.
ALTER TABLE transactions ADD COLUMN merchant_edited_at TIMESTAMPTZ;
