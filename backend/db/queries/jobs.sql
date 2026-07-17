-- name: GetJob :one
SELECT * FROM jobs WHERE id = $1;

-- name: GetJobForUpdate :one
SELECT * FROM jobs WHERE id = $1 FOR UPDATE;

-- name: GetOwnedJob :one
SELECT j.*
FROM jobs j
JOIN projects p ON p.id = j.project_id
WHERE j.id = $1 AND p.user_id = $2;

-- name: ListOwnedProjectJobs :many
SELECT j.*
FROM jobs j
JOIN projects p ON p.id = j.project_id
WHERE j.project_id = $1 AND p.user_id = $2
ORDER BY j.created_at, j.id;

-- name: ListDispatchableJobs :many
SELECT *
FROM jobs
WHERE status = 'queued'
  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
  AND attempt < max_attempts
ORDER BY COALESCE(next_attempt_at, created_at), id
LIMIT $1;

-- name: ListStaleJobs :many
SELECT *
FROM jobs
WHERE status = 'running' AND updated_at < $1
ORDER BY updated_at
LIMIT $2;

-- name: ClaimJob :one
UPDATE jobs
SET status = 'running',
    attempt = sqlc.arg(attempt),
    progress = 0,
    next_attempt_at = NULL,
    error_code = NULL,
    error_message = NULL,
    started_at = COALESCE(started_at, now()),
    finished_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg(job_id)
  AND stage = sqlc.arg(stage)
  AND status = 'queued'
  AND attempt + 1 = sqlc.arg(attempt)
  AND attempt < max_attempts
RETURNING *;

-- name: CreateJobAttempt :one
INSERT INTO job_attempts (id, job_id, attempt, status, worker)
VALUES ($1, $2, $3, 'running', $4)
RETURNING *;

-- name: UpdateJobProgress :one
UPDATE jobs
SET progress = $3, updated_at = now()
WHERE id = $1 AND status = 'running' AND attempt = $2
RETURNING *;

-- name: TouchRunningJob :execrows
UPDATE jobs
SET updated_at = now()
WHERE id = $1 AND status = 'running' AND attempt = $2;

-- name: SucceedJob :one
UPDATE jobs
SET status = 'succeeded',
    progress = 1,
    error_code = NULL,
    error_message = NULL,
    finished_at = now(),
    updated_at = now()
WHERE id = $1 AND status = 'running' AND attempt = $2
RETURNING *;

-- name: SucceedJobAttempt :execrows
UPDATE job_attempts
SET status = 'succeeded', finished_at = now()
WHERE job_id = $1 AND attempt = $2 AND status = 'running';

-- name: RequeueJobAfterFailure :one
UPDATE jobs
SET status = 'queued',
    progress = NULL,
    next_attempt_at = $3,
    error_code = $4,
    error_message = $5,
    updated_at = now()
WHERE id = $1 AND status = 'running' AND attempt = $2
RETURNING *;

-- name: FailJob :one
UPDATE jobs
SET status = 'failed',
    progress = NULL,
    next_attempt_at = NULL,
    error_code = $3,
    error_message = $4,
    finished_at = now(),
    updated_at = now()
WHERE id = $1 AND status = 'running' AND attempt = $2
RETURNING *;

-- name: FailJobAttempt :execrows
UPDATE job_attempts
SET status = 'failed', error_code = $3, error_message = $4, finished_at = now()
WHERE job_id = $1 AND attempt = $2 AND status = 'running';

-- name: CreateNextStageJob :one
INSERT INTO jobs (
  id, project_id, asset_id, stage, status, idempotency_key
)
VALUES ($1, $2, $3, $4, 'queued', $5)
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

-- name: SetProjectReady :exec
UPDATE projects p
SET status = 'ready', updated_at = now()
WHERE p.id = $1
  AND NOT EXISTS (
    SELECT 1 FROM jobs j
    WHERE j.project_id = p.id AND j.status IN ('queued', 'running')
  );

-- name: SetProjectFailed :exec
UPDATE projects SET status = 'failed', updated_at = now() WHERE id = $1;

-- name: RetryOwnedJob :one
UPDATE jobs j
SET status = 'queued',
    max_attempts = j.attempt + 5,
    next_attempt_at = NULL,
    error_code = NULL,
    error_message = NULL,
    finished_at = NULL,
    updated_at = now()
FROM projects p
WHERE j.id = $1
  AND p.id = j.project_id
  AND p.user_id = $2
  AND j.status = 'failed'
RETURNING j.*;
