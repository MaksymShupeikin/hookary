-- +goose Up
CREATE INDEX jobs_dispatchable_idx
    ON jobs ((COALESCE(next_attempt_at, created_at)), id)
    WHERE status = 'queued';

CREATE INDEX jobs_running_updated_idx
    ON jobs (updated_at)
    WHERE status = 'running';

-- +goose Down
DROP INDEX IF EXISTS jobs_running_updated_idx;
DROP INDEX IF EXISTS jobs_dispatchable_idx;
