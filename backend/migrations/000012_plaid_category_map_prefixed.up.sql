-- #181 follow-up: Plaid's /transactions/sync returns personal_finance_category
-- with the *detailed* field prefixed by the *primary*, e.g.
--   primary  = FOOD_AND_DRINK
--   detailed = FOOD_AND_DRINK_FAST_FOOD
-- The 000005 seed used the un-prefixed legacy form (FAST_FOOD), so every
-- lookup in MapPlaidCategory missed and transactions stayed uncategorized
-- despite the PFC flag being correctly requested. Normalize the existing
-- rows to match the wire format.
--
-- The WHERE guard makes this idempotent and safe in case a future seed
-- already lands in the canonical prefixed form.
UPDATE plaid_category_map
   SET plaid_detailed = plaid_primary || '_' || plaid_detailed
 WHERE position(plaid_primary || '_' IN plaid_detailed) <> 1;
