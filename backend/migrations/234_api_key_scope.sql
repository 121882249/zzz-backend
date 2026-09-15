-- Add explicit API key scope. Existing keys remain ordinary fixed-group keys.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS key_type VARCHAR(20) NOT NULL DEFAULT 'group';

UPDATE api_keys
SET key_type = 'group'
WHERE key_type IS NULL OR key_type = '';

CREATE INDEX IF NOT EXISTS idx_api_keys_user_key_type
    ON api_keys(user_id, key_type);

-- A user has at most one active system global key. Ordinary user-created keys
-- may still use the name TokenPro because scope is determined by key_type.
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_one_global_per_user
    ON api_keys(user_id)
    WHERE key_type = 'global' AND deleted_at IS NULL;

ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_key_type_check;
ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_key_type_check
    CHECK (key_type IN ('group', 'global'));

-- Backfill existing users idempotently. The random md5 input combines the
-- user id, clock and PostgreSQL PRNG; the credential is only returned through
-- the normal authenticated API-key endpoint.
INSERT INTO api_keys (user_id, key, name, key_type, status)
SELECT u.id,
       'sk-global-' || md5(random()::text || clock_timestamp()::text || u.id::text),
       'TokenPro',
       'global',
       'active'
FROM users u
WHERE u.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM api_keys k
      WHERE k.user_id = u.id
        AND k.key_type = 'global'
        AND k.deleted_at IS NULL
  );

-- Keep future registrations covered as well. The trigger is intentionally
-- database-side so email, OAuth and admin-created users all get the same
-- idempotent system key without widening every application constructor.
CREATE OR REPLACE FUNCTION ensure_user_global_api_key()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO api_keys (user_id, key, name, key_type, status)
    VALUES (
        NEW.id,
        'sk-global-' || md5(random()::text || clock_timestamp()::text || NEW.id::text),
        'TokenPro',
        'global',
        'active'
    )
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_users_ensure_global_api_key ON users;
CREATE TRIGGER trg_users_ensure_global_api_key
AFTER INSERT ON users
FOR EACH ROW
EXECUTE FUNCTION ensure_user_global_api_key();
