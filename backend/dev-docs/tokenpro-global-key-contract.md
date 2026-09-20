# TokenPro Global Key Contract

The TokenPro global key is the system credential used by desktop and CLI
clients. It is not a normal group-bound API key.

## Identity and lifecycle

- Every non-deleted user has exactly one non-deleted key with
  `key_type = 'global'`.
- `key_type`, not the display name, defines the global key.
- The row ID is stable. Users cannot edit, disable, bind, or delete the key.
- Resetting the key rotates only its credential and invalidates the previous
  credential. Installed clients must reconnect after a reset.
- `GET /api/v1/global-key` is the authoritative credential endpoint. It
  idempotently repairs a missing key and returns `Cache-Control: no-store`.

## Routing

- A global key never stores a group. The database enforces `group_id IS NULL`.
- Every request selects a model/group pair. TokenPro's Codex catalog transports
  that pair as `tp-g<groupID>-<base64url(model)>`; ingress decodes it into the
  internal `X-TokenPro-Group-Id` hint and restores the original model.
- The hint is not authorization. The gateway validates user visibility,
  subscription state, model allowlist, group status, and schedulable accounts
  before attaching the group to the request.
- The internal group header is consumed before upstream forwarding.
- Only an in-request resolution marker permits a second handler pass to reuse a
  group. Persisted or cached legacy `group_id` values are never trusted.

## Client behavior

- Clients fetch the global key immediately before writing channel settings.
- Clients may temporarily fall back to the legacy paginated key lookup only
  when `/api/v1/global-key` is absent on an older server.
- A key reset intentionally makes existing generated provider credentials fail
  authentication. Reconnecting fetches the new key and rewrites the channel
  transactionally; clients must not silently select an ordinary group key.
