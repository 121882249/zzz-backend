-- Repair users whose system global key was deleted before the delete guard
-- shipped. Migration 234 was already recorded for those installations, so its
-- original backfill will not run again during an upgrade.
--
-- The partial unique index from migration 234 guarantees at most one active
-- global key per user. This insert is intentionally idempotent and leaves
-- historical soft-deleted rows untouched for auditability.
INSERT INTO api_keys (user_id, key, name, key_type, status)
SELECT u.id,
       'sk-global-' || md5(random()::text || clock_timestamp()::text || u.id::text),
       'TokenPro',
       'global',
       'active'
FROM users u
WHERE u.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM api_keys k
      WHERE k.user_id = u.id
        AND k.key_type = 'global'
        AND k.deleted_at IS NULL
  )
ON CONFLICT DO NOTHING;
