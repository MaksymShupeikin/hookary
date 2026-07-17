-- name: CreateAsset :one
INSERT INTO assets (
  id, project_id, source_kind, object_key, content_type, size_bytes, metadata
)
VALUES (
  $1, $2, 'r2_upload', $3, $4, $5,
  jsonb_build_object('title', sqlc.arg(filename)::text)
)
RETURNING *;

-- name: CreatePublicURLAsset :one
INSERT INTO assets (
  id, project_id, source_kind, source_url, object_key
)
VALUES ($1, $2, 'public_url', $3, $4)
RETURNING *;

-- name: CreateUpload :one
INSERT INTO uploads (
  id, asset_id, mode, object_key, s3_upload_id, part_size_bytes,
  size_bytes, content_type
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetUpload :one
SELECT
  u.id AS upload_id,
  u.asset_id,
  a.project_id,
  u.mode,
  u.object_key,
  u.s3_upload_id,
  u.part_size_bytes,
  u.size_bytes,
  u.content_type,
  u.parts,
  u.status,
  u.created_at,
  u.updated_at,
  u.completed_at
FROM uploads u
JOIN assets a ON a.id = u.asset_id
JOIN projects p ON p.id = a.project_id
WHERE u.id = $1 AND p.user_id = $2;

-- name: GetOwnedAsset :one
SELECT
  a.*,
  EXISTS (
    SELECT 1
    FROM uploads u
    WHERE u.asset_id = a.id AND u.status = 'completed'
  ) AS upload_completed
FROM assets a
JOIN projects p ON p.id = a.project_id
WHERE a.id = $1 AND p.user_id = $2
LIMIT 1;

-- name: ListProjectAssets :many
SELECT
  a.*,
  (
    a.status = 'ready' OR EXISTS (
      SELECT 1
      FROM uploads u
      WHERE u.asset_id = a.id AND u.status = 'completed'
    )
  ) AS preview_ready,
  EXISTS (
    SELECT 1 FROM transcripts t WHERE t.asset_id = a.id
  ) AS transcript_ready,
  CASE
    WHEN EXISTS (SELECT 1 FROM transcripts t WHERE t.asset_id = a.id)
      THEN 'succeeded'
    ELSE COALESCE(
      (
        SELECT CASE WHEN j.status = 'succeeded' THEN 'failed' ELSE j.status END
        FROM jobs j
        WHERE j.asset_id = a.id AND j.stage = 'analyze'
        ORDER BY j.created_at DESC, j.id DESC
        LIMIT 1
      ),
      CASE WHEN a.status = 'failed' THEN 'failed' ELSE 'pending' END
    )
  END::text AS transcript_status,
  COALESCE(
    (
      SELECT CASE
        WHEN j.status = 'succeeded' AND
          NOT EXISTS (SELECT 1 FROM transcripts t WHERE t.asset_id = a.id)
          THEN 'failed'
        ELSE j.status
      END
      FROM jobs j
      WHERE j.asset_id = a.id AND j.stage = 'analyze'
      ORDER BY j.created_at DESC, j.id DESC
      LIMIT 1
    ),
    CASE WHEN a.status = 'failed' THEN 'failed' ELSE 'pending' END
  )::text AS analysis_status
FROM assets a
JOIN projects p ON p.id = a.project_id
WHERE a.project_id = $1 AND p.user_id = $2
ORDER BY a.created_at DESC, a.id DESC;

-- name: CompleteUpload :one
UPDATE uploads
SET status = 'completed',
    parts = $2,
    completed_at = COALESCE(completed_at, now()),
    updated_at = now()
WHERE id = $1 AND status <> 'aborted'
RETURNING *;

-- name: CreateIngestJob :one
INSERT INTO jobs (
  id, project_id, asset_id, stage, status, idempotency_key
)
VALUES ($1, $2, $3, 'ingest', 'queued', $4)
ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL
DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
RETURNING *;

-- name: ListPendingMultipartUploads :many
SELECT u.id, u.asset_id, u.object_key, u.s3_upload_id
FROM uploads u
JOIN assets a ON a.id = u.asset_id
JOIN projects p ON p.id = a.project_id
WHERE p.id = $1
  AND p.user_id = $2
  AND u.mode = 'multipart'
  AND u.status = 'pending';

-- name: DeletePendingUploadAsset :execrows
DELETE FROM assets a
USING uploads u, projects p
WHERE u.id = $1
  AND u.asset_id = a.id
  AND p.id = a.project_id
  AND p.user_id = $2
  AND u.status = 'pending';

-- name: GetAssetForIngest :one
SELECT *
FROM assets
WHERE id = $1 AND project_id = $2;

-- name: CompleteIngestAsset :one
UPDATE assets
SET object_key = $3,
    content_type = $4,
    size_bytes = $5,
    duration_ms = $6,
    width = $7,
    height = $8,
    checksum = $9,
    metadata = metadata || sqlc.arg(metadata)::jsonb,
    status = 'ready',
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: FailIngestAsset :execrows
UPDATE assets
SET status = 'failed', updated_at = now()
WHERE id = $1 AND project_id = $2 AND status <> 'ready';
