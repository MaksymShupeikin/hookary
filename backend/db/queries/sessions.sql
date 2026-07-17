-- name: CreateSession :one
INSERT INTO sessions (
  id, user_id, refresh_token_hash, rotated_from, user_agent, ip, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSessionByTokenHash :one
-- Looks up by hash, because the token itself is never stored.
SELECT * FROM sessions WHERE refresh_token_hash = $1;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionTree :exec
-- Revokes a session and every session rotated out of it.
--
-- Used when an already-rotated token is presented again: the only ways that
-- happens are a leak or a replay, and in both cases every session descended
-- from it is suspect. Revoking just the one presented would leave the thief's
-- newer session live.
WITH RECURSIVE tree AS (
  SELECT sessions.id FROM sessions WHERE sessions.id = $1
  UNION ALL
  SELECT s.id FROM sessions s JOIN tree t ON s.rotated_from = t.id
)
UPDATE sessions SET revoked_at = now()
WHERE sessions.id IN (SELECT tree.id FROM tree)
  AND sessions.revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE sessions SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :execrows
-- Expired rows are dead weight and still hold a token hash; nothing reads them.
DELETE FROM sessions WHERE expires_at < now();
