-- A global key identifies a user. Its group is selected and authorized for
-- each request, so persisted group state is both stale and unsafe.
UPDATE api_keys
SET group_id = NULL
WHERE key_type = 'global'
  AND group_id IS NOT NULL;

ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_global_group_null_check;
ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_global_group_null_check
    CHECK (key_type <> 'global' OR group_id IS NULL);
