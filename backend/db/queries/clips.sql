-- name: CountAssetClips :one
SELECT count(*)
FROM clips
WHERE asset_id = $1;

-- name: CreateClipCandidate :one
INSERT INTO clip_candidates (
  id, project_id, asset_id, start_ms, end_ms, score, hook, title, reason,
  context_risk, selected, model, prompt_version
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, true, $11, $12)
RETURNING *;

-- name: CreateClip :one
INSERT INTO clips (
  id, project_id, asset_id, candidate_id, title, hook, reason, score,
  context_risk, start_ms, end_ms
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListOwnedProjectClips :many
SELECT c.*
FROM clips c
JOIN projects p ON p.id = c.project_id
WHERE c.project_id = $1 AND p.user_id = $2
ORDER BY c.score DESC, c.created_at, c.id;

-- name: GetOwnedProjectClip :one
SELECT c.*
FROM clips c
JOIN projects p ON p.id = c.project_id
WHERE c.id = $1 AND c.project_id = $2 AND p.user_id = $3;

-- name: GetOwnedClip :one
SELECT c.*
FROM clips c
JOIN projects p ON p.id = c.project_id
WHERE c.id = $1 AND p.user_id = $2;

-- name: GetClipForRender :one
SELECT *
FROM clips
WHERE id = $1 AND project_id = $2;

-- name: QueueClipRenderJob :one
INSERT INTO jobs (
  id, project_id, asset_id, clip_id, stage, status, idempotency_key
)
VALUES ($1, $2, $3, $4, 'render', 'queued', $5)
ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL
DO UPDATE SET
  status = CASE WHEN jobs.status = 'failed' THEN 'queued' ELSE jobs.status END,
  max_attempts = CASE
    WHEN jobs.status = 'failed' THEN GREATEST(jobs.max_attempts, jobs.attempt + 5)
    ELSE jobs.max_attempts
  END,
  next_attempt_at = CASE WHEN jobs.status = 'failed' THEN NULL ELSE jobs.next_attempt_at END,
  error_code = CASE WHEN jobs.status = 'failed' THEN NULL ELSE jobs.error_code END,
  error_message = CASE WHEN jobs.status = 'failed' THEN NULL ELSE jobs.error_message END,
  finished_at = CASE WHEN jobs.status = 'failed' THEN NULL ELSE jobs.finished_at END,
  updated_at = now()
RETURNING *;

-- name: MarkClipRendering :one
UPDATE clips
SET status = 'rendering', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CompleteClipRender :one
UPDATE clips
SET status = 'rendered',
    output_key = $2,
    thumbnail_key = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: FailClipRender :execrows
UPDATE clips
SET status = CASE WHEN output_key IS NULL THEN 'failed' ELSE 'rendered' END,
    updated_at = now()
WHERE id = $1 AND status = 'rendering';
