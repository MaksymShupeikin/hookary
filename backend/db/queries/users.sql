-- name: UpsertUserByGoogleSub :one
-- Conflict is on google_sub, never on email: an account's email can change,
-- its sub cannot. Matching on email would hand the account to whoever inherits
-- the address.
INSERT INTO users (id, google_sub, email, display_name, avatar_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (google_sub) DO UPDATE
SET email        = EXCLUDED.email,
    display_name = EXCLUDED.display_name,
    avatar_url   = EXCLUDED.avatar_url,
    updated_at   = now()
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;
