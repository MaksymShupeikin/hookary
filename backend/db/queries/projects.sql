-- name: CreateProject :one
INSERT INTO projects (id, user_id, title)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListProjects :many
-- ULIDs are minted immediately before insert, so id is the stable pagination
-- cursor as well as the tie-breaker. A cursor never exposes an offset that can
-- drift while another project is created.
SELECT
  p.id,
  p.title,
  p.status,
  p.created_at,
  p.updated_at,
  count(DISTINCT a.id)::bigint AS asset_count,
  count(DISTINCT c.id)::bigint AS clip_count
FROM projects p
LEFT JOIN assets a ON a.project_id = p.id
LEFT JOIN clips c ON c.project_id = p.id
WHERE p.user_id = $1
  AND ($2::text = '' OR p.id < $2::ulid)
GROUP BY p.id
ORDER BY p.id DESC
LIMIT $3;

-- name: GetProject :one
SELECT
  p.id,
  p.title,
  p.status,
  p.created_at,
  p.updated_at,
  count(DISTINCT a.id)::bigint AS asset_count,
  count(DISTINCT c.id)::bigint AS clip_count
FROM projects p
LEFT JOIN assets a ON a.project_id = p.id
LEFT JOIN clips c ON c.project_id = p.id
WHERE p.id = $1 AND p.user_id = $2
GROUP BY p.id;

-- name: SetProjectProcessing :exec
UPDATE projects
SET status = 'processing', updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = $1 AND user_id = $2;

-- name: GetProjectOwnerID :one
SELECT user_id FROM projects WHERE id = $1;
