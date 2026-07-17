# Hookary

Turns a long video into 3–5 short vertical clips with subtitles.

```
Google Sign-In → create project → upload a video or paste a public URL
→ wait for processing → get 3–5 suggested clips → preview vertical
with subtitles → download
```

**Status: early.** Google Sign-In, project CRUD, direct upload, YouTube import,
the durable queue, captions-first transcription, Whisper fallback, and
structured clip selection, user-controlled 9:16 rendering, timed burned-in
subtitles, and direct presigned downloads are in place. OAuth setup is
documented in [docs/google-auth-setup.md](docs/google-auth-setup.md). YouTube
publishing remains a worker skeleton; see [Roadmap](#roadmap).

## Requirements

| Tool | Version used |
|---|---|
| Go | 1.26 |
| Flutter | 3.44 (Dart 3.12) |
| Docker | with Compose v2+ |

FFmpeg and yt-dlp are **not** needed on your machine — they live in the ingest
and render images.

## Getting started

```sh
make setup       # .env from .env.example, plus flutter pub get
make up          # build and start web, postgres, nats, minio, and five services
make migrate-up  # create the schema
make health      # all five should answer 200
```

Open Hookary at [http://localhost:8000](http://localhost:8000). nginx serves the
Flutter SPA and proxies `/v1` and `/health` to the API inside Compose, so the
browser never needs to resolve Docker service names.

`make setup` writes `.env` with working local defaults. Google OAuth is needed
for sign-in. `OPENAI_API_KEY` is needed for local-file transcription and for
selecting clip candidates; YouTube captions can still be extracted without it.
The stack starts and stays healthy with the key absent, but analyze jobs stop
with `ai_not_configured` before model work.

For host-side Flutter hot reload instead of the Docker web service, use:

```sh
make run-web
```

## Layout

```
frontend/   Flutter — web, iOS, Android
backend/    Five Go services + shared helpers, one go.mod
contracts/  openapi.yaml and events.md — the API contract
deploy/     Dockerfiles and Cloudflare R2 setup
```

Read `AGENTS.md` before writing code — the root one for architecture, and the
one in `frontend/` or `backend/` for the rules that apply where you are working.

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

| Service | Port | Does |
|---|---|---|
| api | 8080 | REST + WebSocket; the only process clients talk to |
| ingest | 8081 | uploads and public URLs → a normalized source asset |
| analyze | 8082 | audio → transcript → clip candidates |
| render | 8083 | candidate → vertical MP4 with subtitles |
| publish | 8084 | clip → YouTube |

Local storage is MinIO, standing in for Cloudflare R2. Same S3 API, so only the
`S3_*` values change in production. `S3_ENDPOINT` is used server-side;
`S3_PUBLIC_ENDPOINT` is embedded into presigned URLs and must be reachable by
the browser. For R2 both values are its S3 API endpoint, never a custom domain.

## Commands

```sh
make help            # every target
make up              # start the stack
make down            # stop it, keep data
make reset           # stop it, delete data
make logs            # tail everything
make check           # lint + test, what CI runs
make migrate-up      # apply migrations
make migrate-status  # what has been applied
```

Migrations never run automatically on startup: a schema change is a deliberate
step, not a side effect of a deploy.

## Roadmap

| Stage | What | Status |
|---|---|---|
| 1 | Monorepo, rules, env | done |
| 2 | Docker Compose stack | done |
| 3 | Go API skeleton — config, pgx, migrations, NATS, R2, middleware | done |
| 4 | Google auth, sessions, token rotation | done — needs OAuth client ids |
| 5 | Projects and direct-to-R2 upload | done |
| 6 | Job queue and state machine | done |
| 7 | Ingest worker | done |
| 8 | OpenAI analyze worker | done |
| 9 | Render worker | done — full-width 16:9 video on a 9:16 ambient canvas, ASS subtitles, MP4 download |
| 10 | Flutter vertical slice | in progress |
| 11 | YouTube publisher | |
| 12 | Tests and performance pass | |
