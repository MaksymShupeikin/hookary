-- +goose Up

-- Ids are ULIDs: time-sortable, so an index on id is already roughly an index
-- on creation order. The domain makes a malformed id fail at write time rather
-- than turning into a silent empty result at read time. The pattern matches the
-- one in contracts/openapi.yaml.
CREATE DOMAIN ulid AS text
  CHECK (VALUE ~ '^[0-9A-HJKMNP-TV-Z]{26}$');


-- ---------------------------------------------------------------- users

CREATE TABLE users (
  id            ulid PRIMARY KEY,
  -- Google's subject id, not the email: an account's email can change, its sub
  -- cannot. Matching on email would hand the account to whoever inherits it.
  google_sub    text NOT NULL UNIQUE,
  email         text NOT NULL UNIQUE,
  display_name  text NOT NULL,
  avatar_url    text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);


CREATE TABLE sessions (
  id                  ulid PRIMARY KEY,
  user_id             ulid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  -- The token itself is never stored. A database leak must not hand out live
  -- sessions.
  refresh_token_hash  bytea NOT NULL UNIQUE,
  -- The session this one was rotated from. Presenting an already-rotated token
  -- means it leaked, and the whole chain gets revoked.
  rotated_from        ulid REFERENCES sessions(id) ON DELETE SET NULL,
  user_agent          text,
  ip                  inet,
  expires_at          timestamptz NOT NULL,
  revoked_at          timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_expires_idx ON sessions (user_id, expires_at);


-- ------------------------------------------------------------- projects

CREATE TABLE projects (
  id          ulid PRIMARY KEY,
  user_id     ulid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  status      text NOT NULL DEFAULT 'empty'
              CHECK (status IN ('empty', 'processing', 'ready', 'failed')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX projects_user_created_idx ON projects (user_id, created_at DESC);


CREATE TABLE assets (
  id            ulid PRIMARY KEY,
  project_id    ulid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_kind   text NOT NULL CHECK (source_kind IN ('r2_upload', 'public_url')),
  -- Set for public_url imports only; kept for provenance.
  source_url    text,
  object_key    text NOT NULL,
  content_type  text,
  size_bytes    bigint CHECK (size_bytes IS NULL OR size_bytes > 0),
  duration_ms   bigint,
  width         integer,
  height        integer,
  -- Content hash of the source. Re-analyzing the same bytes must hit the cache
  -- rather than pay OpenAI twice.
  checksum      text,
  status        text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'ready', 'failed')),
  -- ffprobe output and other small extras. Anything large belongs in R2.
  metadata      jsonb NOT NULL DEFAULT '{}',
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX assets_project_created_idx ON assets (project_id, created_at);
CREATE INDEX assets_checksum_idx ON assets (checksum) WHERE checksum IS NOT NULL;


-- -------------------------------------------------------------- uploads

CREATE TABLE uploads (
  id              ulid PRIMARY KEY,
  asset_id        ulid NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  mode            text NOT NULL CHECK (mode IN ('single_put', 'multipart')),
  object_key      text NOT NULL,
  -- S3 multipart UploadId. Needed to complete or abort; null for single_put.
  s3_upload_id    text,
  part_size_bytes integer,
  size_bytes      bigint NOT NULL CHECK (size_bytes > 0),
  content_type    text NOT NULL,
  -- Completed parts as [{"part_number": 1, "etag": "..."}]. Bounded by S3's
  -- 10k part limit, so it stays small enough for jsonb.
  parts           jsonb NOT NULL DEFAULT '[]',
  status          text NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'completed', 'aborted')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  completed_at    timestamptz,

  CONSTRAINT uploads_multipart_has_s3_id
    CHECK (mode <> 'multipart' OR s3_upload_id IS NOT NULL)
);

-- Finds abandoned multipart sessions to abort, so orphaned parts stop being
-- billed.
CREATE INDEX uploads_pending_idx ON uploads (status, created_at)
  WHERE status = 'pending';


-- ----------------------------------------------------------------- jobs

CREATE TABLE jobs (
  id              ulid PRIMARY KEY,
  project_id      ulid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_id        ulid REFERENCES assets(id) ON DELETE CASCADE,
  -- Set for render and publish jobs. The FK is added with the clips table.
  clip_id         ulid,
  stage           text NOT NULL
                  CHECK (stage IN ('ingest', 'analyze', 'render', 'publish')),
  status          text NOT NULL DEFAULT 'queued'
                  CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
  progress        real CHECK (progress IS NULL OR (progress >= 0 AND progress <= 1)),
  attempt         integer NOT NULL DEFAULT 0,
  max_attempts    integer NOT NULL DEFAULT 5,
  next_attempt_at timestamptz,
  -- Makes a retried enqueue collapse onto the same job instead of starting a
  -- second render.
  idempotency_key text,
  error_code      text,
  error_message   text,

  -- OpenAI spend, aggregated per job. The budget guardrail sums this column,
  -- so it has to be cheap to add up.
  ai_input_tokens  bigint NOT NULL DEFAULT 0,
  ai_output_tokens bigint NOT NULL DEFAULT 0,
  ai_cost_usd      numeric(12, 6) NOT NULL DEFAULT 0,
  -- Which prompt and model snapshot produced this job's output. Without these
  -- a bad batch cannot be traced back to what generated it.
  prompt_version   text,
  model            text,

  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  started_at      timestamptz,
  finished_at     timestamptz
);

CREATE UNIQUE INDEX jobs_idempotency_key_idx ON jobs (idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE INDEX jobs_project_status_created_idx ON jobs (project_id, status, created_at);

-- The claim query: what is due to run now.
CREATE INDEX jobs_status_next_attempt_idx ON jobs (status, next_attempt_at);


CREATE TABLE job_attempts (
  id             ulid PRIMARY KEY,
  job_id         ulid NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  attempt        integer NOT NULL,
  status         text NOT NULL
                 CHECK (status IN ('running', 'succeeded', 'failed')),
  error_code     text,
  error_message  text,
  -- Which worker instance ran it, for tracing a failure to a container.
  worker         text,
  started_at     timestamptz NOT NULL DEFAULT now(),
  finished_at    timestamptz,

  UNIQUE (job_id, attempt)
);


-- ---------------------------------------------------------- transcripts

CREATE TABLE transcripts (
  id           ulid PRIMARY KEY,
  asset_id     ulid NOT NULL UNIQUE REFERENCES assets(id) ON DELETE CASCADE,
  language     text,
  duration_ms  bigint,
  -- The word-level JSON lives gzipped in R2. Storing a row per word would put
  -- millions of rows in Postgres to answer queries nobody makes.
  object_key   text NOT NULL,
  checksum     text NOT NULL,
  word_count   integer,
  model        text NOT NULL,
  version      integer NOT NULL DEFAULT 1,
  created_at   timestamptz NOT NULL DEFAULT now()
);


-- ------------------------------------------------------------ candidates

CREATE TABLE clip_candidates (
  id            ulid PRIMARY KEY,
  project_id    ulid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_id      ulid NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  start_ms      bigint NOT NULL,
  end_ms        bigint NOT NULL,
  score         real NOT NULL CHECK (score >= 0 AND score <= 1),
  hook          text,
  title         text,
  reason        text,
  context_risk  text CHECK (context_risk IN ('low', 'medium', 'high')),
  -- True once the final pass promoted this window to a clip.
  selected      boolean NOT NULL DEFAULT false,
  model         text,
  prompt_version text,
  created_at    timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT clip_candidates_range CHECK (end_ms > start_ms)
);

CREATE INDEX clip_candidates_asset_score_idx
  ON clip_candidates (asset_id, score DESC);


-- ---------------------------------------------------------------- clips

CREATE TABLE clips (
  id             ulid PRIMARY KEY,
  project_id     ulid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_id       ulid NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  candidate_id   ulid REFERENCES clip_candidates(id) ON DELETE SET NULL,
  title          text NOT NULL DEFAULT '',
  hook           text,
  reason         text,
  score          real NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 1),
  context_risk   text CHECK (context_risk IN ('low', 'medium', 'high')),
  start_ms       bigint NOT NULL,
  end_ms         bigint NOT NULL,
  status         text NOT NULL DEFAULT 'candidate'
                 CHECK (status IN ('candidate', 'rendering', 'rendered', 'failed')),

  -- Render framing. focus_x/focus_y are stored now and only centered on; a CV
  -- worker will fill them in later without a schema change.
  crop_mode      text NOT NULL DEFAULT 'center'
                 CHECK (crop_mode IN ('center', 'blur_pad')),
  focus_x        real NOT NULL DEFAULT 0.5 CHECK (focus_x >= 0 AND focus_x <= 1),
  focus_y        real NOT NULL DEFAULT 0.5 CHECK (focus_y >= 0 AND focus_y <= 1),

  output_key     text,
  thumbnail_key  text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT clips_range CHECK (end_ms > start_ms)
);

CREATE INDEX clips_project_score_idx ON clips (project_id, score DESC);

ALTER TABLE jobs
  ADD CONSTRAINT jobs_clip_id_fkey
  FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE;


-- ----------------------------------------------------------- publishing

CREATE TABLE platform_connections (
  id                      ulid PRIMARY KEY,
  user_id                 ulid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform                text NOT NULL CHECK (platform IN ('youtube')),
  external_account_id     text,
  account_name            text,
  -- Encrypted with PLATFORM_TOKEN_ENCRYPTION_KEY before it ever reaches this
  -- table. A dump must not yield usable access to anyone's channel.
  access_token_encrypted  bytea,
  refresh_token_encrypted bytea NOT NULL,
  token_expires_at        timestamptz,
  -- The granted scopes, which are the upload grant and deliberately not the
  -- login scopes.
  scopes                  text[] NOT NULL DEFAULT '{}',
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),

  UNIQUE (user_id, platform)
);


CREATE TABLE publications (
  id              ulid PRIMARY KEY,
  clip_id         ulid NOT NULL REFERENCES clips(id) ON DELETE CASCADE,
  user_id         ulid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform        text NOT NULL CHECK (platform IN ('youtube')),
  status          text NOT NULL DEFAULT 'queued'
                  CHECK (status IN ('queued', 'uploading', 'published', 'failed')),
  external_id     text,
  external_url    text,
  -- Required, not optional: a duplicate upload to someone's channel cannot be
  -- undone from our side.
  idempotency_key text NOT NULL UNIQUE,
  error_code      text,
  error_message   text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Catches a double upload of the same video: nulls do not conflict, so rows
-- still pending publication are unconstrained.
CREATE UNIQUE INDEX publications_platform_external_idx
  ON publications (platform, external_id)
  WHERE external_id IS NOT NULL;


-- +goose Down

DROP TABLE IF EXISTS publications;
DROP TABLE IF EXISTS platform_connections;
ALTER TABLE IF EXISTS jobs DROP CONSTRAINT IF EXISTS jobs_clip_id_fkey;
DROP TABLE IF EXISTS clips;
DROP TABLE IF EXISTS clip_candidates;
DROP TABLE IF EXISTS transcripts;
DROP TABLE IF EXISTS job_attempts;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS uploads;
DROP TABLE IF EXISTS assets;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP DOMAIN IF EXISTS ulid;
