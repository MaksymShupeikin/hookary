-- name: GetAssetForAnalyze :one
SELECT *
FROM assets
WHERE id = $1 AND project_id = $2;

-- name: GetTranscriptByAsset :one
SELECT *
FROM transcripts
WHERE asset_id = $1;

-- name: GetOwnedAssetTranscript :one
SELECT t.*
FROM transcripts t
JOIN assets a ON a.id = t.asset_id
JOIN projects p ON p.id = a.project_id
WHERE t.asset_id = $1 AND p.user_id = $2;

-- name: UpsertTranscript :one
INSERT INTO transcripts (
  id, asset_id, language, duration_ms, object_key, checksum, word_count, model, version
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (asset_id) DO UPDATE SET
  language = EXCLUDED.language,
  duration_ms = EXCLUDED.duration_ms,
  object_key = EXCLUDED.object_key,
  checksum = EXCLUDED.checksum,
  word_count = EXCLUDED.word_count,
  model = EXCLUDED.model,
  version = EXCLUDED.version
RETURNING *;

-- name: TotalAICostUSD :one
SELECT COALESCE(SUM(ai_cost_usd), 0)::double precision
FROM jobs;

-- name: AddJobAICost :exec
UPDATE jobs
SET ai_cost_usd = ai_cost_usd + sqlc.arg(cost_usd)::double precision,
    model = sqlc.arg(model),
    updated_at = now()
WHERE id = sqlc.arg(job_id);

-- name: AddJobAIUsage :exec
UPDATE jobs
SET ai_input_tokens = ai_input_tokens + sqlc.arg(input_tokens),
    ai_output_tokens = ai_output_tokens + sqlc.arg(output_tokens),
    ai_cost_usd = ai_cost_usd + sqlc.arg(cost_usd)::double precision,
    prompt_version = sqlc.arg(prompt_version),
    model = sqlc.arg(model),
    updated_at = now()
WHERE id = sqlc.arg(job_id);

-- name: QueueAssetReingest :one
INSERT INTO jobs (
  id, project_id, asset_id, stage, status, idempotency_key
)
VALUES ($1, $2, $3, 'ingest', 'queued', $4)
ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL
DO UPDATE SET
  status = CASE
    WHEN jobs.status IN ('failed', 'succeeded') THEN 'queued'
    ELSE jobs.status
  END,
  max_attempts = GREATEST(jobs.max_attempts, jobs.attempt + 5),
  next_attempt_at = CASE
    WHEN jobs.status IN ('failed', 'succeeded') THEN NULL
    ELSE jobs.next_attempt_at
  END,
  error_code = CASE
    WHEN jobs.status IN ('failed', 'succeeded') THEN NULL
    ELSE jobs.error_code
  END,
  error_message = CASE
    WHEN jobs.status IN ('failed', 'succeeded') THEN NULL
    ELSE jobs.error_message
  END,
  finished_at = CASE
    WHEN jobs.status IN ('failed', 'succeeded') THEN NULL
    ELSE jobs.finished_at
  END,
  updated_at = now()
RETURNING *;
