-- Reverse of 000012: strip the leading "<primary>_" from plaid_detailed for
-- any row where it was added. Only touches rows that begin with the
-- exact prefix so manually-added rows in another shape are untouched.
UPDATE plaid_category_map
   SET plaid_detailed = substr(plaid_detailed, length(plaid_primary) + 2)
 WHERE position(plaid_primary || '_' IN plaid_detailed) = 1;
