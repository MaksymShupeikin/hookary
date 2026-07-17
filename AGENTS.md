# Hookary

Turns a long video into 3–5 short vertical clips with subtitles.

`Google Sign-In → create project → upload a video or paste a public URL →
wait for processing → get 3–5 suggested clips → preview vertical with
subtitles → download.`

## Spelling

**Hookary**, always. Not Hookory. It appears in the Go module path, the R2
bucket names, the `com.hookary.app` bundle id, NATS subjects, and the OAuth
client registrations, so a second spelling is a real bug, not a typo.

## Layout

```
frontend/   Flutter — web, iOS, Android
backend/    Five Go services + shared helpers, one go.mod
contracts/  openapi.yaml and events.md — the API contract
deploy/     Dockerfiles and Cloudflare setup
```

`frontend/AGENTS.md` and `backend/AGENTS.md` carry the rules that matter when
writing code in each. Read the one you are working in.

## Architecture

```
Flutter Web / iOS / Android
              │
         REST + WebSocket
              │
          hookary-api
       ┌──────┼─────────┐
  PostgreSQL  NATS    Cloudflare R2
              │
      ┌───────┼────────┬─────────┐
    ingest  analyze  render   publish
```

One monorepo. Five small Go processes. One PostgreSQL. Large data only in R2.
NATS JetStream for background work.

## Rules that hold everywhere

These are the ones worth stating because a reasonable person would do otherwise:

- **Video bytes never pass through the Go API.** Flutter uploads straight to R2
  with a presigned URL, and downloads the same way. The API mints URLs and
  records state; it does not proxy media.
- **Flutter never sees R2 credentials.** Only the API holds them.
- **NATS messages carry ids, not payloads.** No video, no transcript. A worker
  loads what it needs from Postgres and R2.
- **Transcript words are not Postgres rows.** The full word-timestamp JSON is
  gzipped in R2; Postgres stores the key, language, duration, and checksum.
- **Every job is idempotent.** A redelivered message must not produce a second
  render or a second publication. Check state before acting.
- **Nothing loads a whole video into memory.** Not Dart, not Go.
- **A presigned URL is a bearer token.** Short TTL, never logged.

## Rules about how this code is written

The point is that a new file should look like the ones next to it.

- **No Clean Architecture.** No `domain/`, `application/`, `infrastructure/`,
  `usecases/`, `repositories/`, `entities/`, or `presentation/` directories.
- **No repository interfaces** invented so a struct can be mocked. Write the
  query. Introduce an interface when there are genuinely two implementations.
- **One model per concept.** Not a DTO plus an Entity plus a DomainModel plus a
  DatabaseModel of the same thing.
- **Flat files inside each service and each feature.** Split a file when it
  reaches roughly 300–400 lines, not before, and split it by what it does.

Why: this is a small team shipping an MVP. Layers that pay off across many teams
and many years cost more than they return here, and the cost is paid on every
change.

## Not in the MVP

Payments, teams, a frame-level editor, model training, face tracking,
Kubernetes, a database per service, and gRPC between services. TikTok and X come
after YouTube publishing is stable.

## Getting started

```sh
make setup     # .env + flutter pub get
make up        # whole backend stack
make health    # all five services should answer 200
make check     # what CI runs
```
