# Hookary backend

Five processes, one module, one `go.mod`.

```
shared/              helpers every service uses — a toolbox, not a layer
  config.go            typed config, read once, validated at startup
  logging.go           zerolog: JSON in prod, console in dev
  service.go           Bootstrap, ShutdownContext, Connect, Deps
  database.go          pgxpool + its readiness probe
  nats.go              JetStream connection, subjects, probe
  r2.go                S3 client for R2 and MinIO, probe
  health.go            /health/live and /health/ready handlers
  ulid.go              NewID, ValidID
migrations/          goose SQL migrations
services/api/        REST + WebSocket, the only process clients talk to
services/ingest/     uploads and public URLs -> a normalized source asset
services/analyze/    audio -> transcript -> clip candidates
services/render/     candidate -> vertical MP4 with subtitles
services/publish/    clip -> YouTube
```

## Files are flat

Inside a service, name files after what they do:

```
services/analyze/
├── main.go
├── consumer.go
├── transcript.go
├── candidates.go
├── ranking.go
└── prompts.go
```

Do not create `domain/`, `application/`, `infrastructure/`, `usecases/`,
`repositories/`, `entities/`, or `presentation/`. Do not write an interface so
that a struct can be mocked — write the query, and add an interface only when a
second real implementation exists.

Split a file at roughly 300–400 lines, by responsibility.

## Libraries

| Job | Choice |
|---|---|
| HTTP | `net/http` + `go-chi/chi` |
| Postgres | `pgx/v5` + `pgxpool` |
| Queries | `sqlc` |
| Migrations | `pressly/goose` |
| Queue | `nats.go` + JetStream |
| R2 | `aws-sdk-go-v2/service/s3` |
| OpenAI | `openai-go/v3` |
| JWT | `golang-jwt/jwt/v5` |
| Google OAuth | `golang.org/x/oauth2` |
| Ids | ULID |
| Video | system `ffmpeg` / `ffprobe` |
| URL import | `yt-dlp` binary |

No ORM. `pgx` + `sqlc` is faster, and it never issues a query you did not write.

chi rather than Fiber: the load lives in R2, FFmpeg, and OpenAI, not in
routing, and chi stays compatible with plain `net/http` middleware, profilers,
and streaming.

## Rules

- **Never build a shell command by concatenating user input.** `yt-dlp` and
  `ffmpeg` get their arguments as separate `exec.Command` arguments. A URL is
  data, not shell.
- **Validate every import URL before fetching it.** Reject localhost, private
  ranges, and cloud metadata endpoints, or the ingest worker becomes an SSRF
  proxy into the network.
- **Check job state before doing the work.** JetStream delivers at least once;
  the second delivery must be a no-op, not a second render.
- **Ack only after the result is written.** An early ack loses the job on crash.
- **FFmpeg runs only in ingest and render.** Those two have the media binaries;
  no other image does, and it should stay that way.
- **Bound the concurrency.** FFmpeg will take every core it is given.
  `max_render_jobs = max(1, CPU_COUNT / 2)`.
- **The pool has an explicit ceiling.** Five services share one Postgres.
- **Record token usage and estimated cost on every OpenAI call.** When the
  budget is spent, fail with `budget_exhausted` rather than retrying.
- **Pin model snapshots in env, never in code**, and record the pin on the job
  along with the prompt version.

## Startup

Every service does the same four things, in this order:

```go
cfg, logger, err := shared.Bootstrap("api")   // config + zerolog
ctx, stop := shared.ShutdownContext()          // cancelled on SIGTERM
deps, closeDeps, err := shared.Connect(...)    // postgres, nats, storage
defer closeDeps()                              // released in reverse order
```

`Connect` is all-or-nothing: if one dependency fails, the ones already open are
closed before it returns.

## Health

Every service, including the workers, serves `/health/live` and `/health/ready`.
Liveness ignores dependencies; readiness runs `deps.Ready()`. A failing
dependency means "stop sending me traffic", not "restart me" — conflating the
two turns a database blip into five crash-looping containers.

## Schema

One migration, applied by hand:

```sh
make migrate-up
make migrate-status
make migrate-create name=add_widgets
```

Ids are ULIDs from `shared.NewID()`. The `ulid` domain enforces the format in
the database, so a malformed id fails on write instead of silently matching
nothing on read.

`updated_at` is set explicitly by each query, not by a trigger. Triggers are
the same hidden behaviour that ruled out an ORM.

## Commands

```sh
make go-build
make go-test
make go-lint     # go vet + gofmt check
```
