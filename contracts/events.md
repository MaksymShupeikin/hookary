# NATS JetStream contract

## Subjects

```
hookary.jobs.ingest       api      -> ingest
hookary.jobs.analyze      ingest   -> analyze
hookary.jobs.render       api      -> render   (only user-selected clips)
hookary.jobs.publish      api      -> publish  (explicit publish action)
hookary.events.job-updated  any    -> api (fans out to WebSocket clients)
hookary.dlq.<stage>       any      -> nobody; parked for a human
```

## Message

Every job message is the same shape. It carries **ids only** — no video, no
transcript, no large metadata. A worker loads what it needs from Postgres and
R2. A message that grows a payload field is a bug: JetStream is not a data
store, and a fat message makes redelivery expensive.

```json
{
  "job_id": "01JZ8X...",
  "project_id": "01JZ8W...",
  "asset_id": "01JZ8V...",
  "attempt": 1,
  "created_at": "2026-07-16T09:15:00Z"
}
```

| Field | Notes |
|---|---|
| `job_id` | ULID. The idempotency key for the whole stage. |
| `project_id` | ULID. Ownership checks and WebSocket routing. |
| `asset_id` | ULID. Absent on publish jobs, which key off `clip_id`. |
| `attempt` | 1-based. Set by the publisher, not the consumer. |
| `created_at` | RFC 3339 UTC. When the job was enqueued, not when sent. |

## Delivery rules

Postgres is the source of truth. The API dispatcher repeatedly publishes due
`queued` rows and uses `job_id:attempt` as `Nats-Msg-Id`. JetStream's duplicate
window collapses the harmless repeat. This closes the crash gap between a DB
commit and a publish: a committed job is never lost just because the API died
before sending it.

JetStream delivers **at least once**. Every consumer must therefore:

1. **Load the job row first and check its state.** If it is already `succeeded`,
   ack and return. A redelivered message must never produce a second render or
   a second publication.
2. **Ack only after the result is committed.** Acking first loses the job if the
   worker dies mid-work; that is the one failure mode restart cannot fix.
3. **Use a durable consumer**, one per worker, so a restart resumes rather than
   replays from the beginning.
4. **Back off exponentially** on retry, and stop at the attempt limit.
5. **Publish to `hookary.dlq.<stage>`** on final failure, with the job id and
   the last error. Nothing consumes the DLQ automatically.

## Progress events

`hookary.events.job-updated` carries state transitions only, never payloads.
The API subscribes and forwards to `/v1/ws` clients watching that project.

```json
{
  "job_id": "01JZ8X...",
  "project_id": "01JZ8W...",
  "status": "running",
  "stage": "analyze",
  "progress": 0.42,
  "attempt": 1,
  "error": null,
  "created_at": "2026-07-16T09:15:00Z",
  "updated_at": "2026-07-16T09:16:11Z"
}
```

`status` is one of `queued`, `running`, `succeeded`, `failed`. `progress` is
optional and advisory — a client must not infer completion from it, only from
`status`.

Only ingest automatically creates the analyze job. Analyze stops after saving
suggestions. Render and publish jobs are created by authenticated API actions;
this prevents unwanted suggestions from consuming FFmpeg time or being sent to
a platform.

## Job state machine

```
queued ──> running ──> succeeded
             │
             ├──> failed        (attempts exhausted; DLQ)
             └──> queued        (retry, attempt + 1)
```

Transitions are guarded by the current state in the `UPDATE`, so two workers
racing on the same job cannot both win.

A running row is a lease. The worker updates `updated_at` and calls JetStream
`InProgress`; if both stop for 30 seconds, the dispatcher records the attempt
as failed and either requeues it with exponential backoff or moves the job to
its stage DLQ after the attempt limit.

## WebSocket protocol

The browser connects to `/v1/ws` and immediately sends:

```json
{"type":"authenticate","access_token":"ey...","project_id":"01JZ8W..."}
```

After ownership is checked, the server first returns the durable snapshot:

```json
{"type":"snapshot","jobs":[]}
```

Subsequent messages contain one transition:

```json
{"type":"job_updated","job":{"id":"01JZ8X...","project_id":"01JZ8W...","stage":"ingest","status":"running","progress":0.25,"attempt":1,"error":null,"created_at":"2026-07-16T09:15:00Z","updated_at":"2026-07-16T09:15:02Z"}}
```

The WebSocket is an optimization. Reconnects and slow clients recover from the
REST snapshot rather than asking NATS to replay UI events.

## Dead letters

`hookary.dlq.<stage>` stores a compact terminal record:

```json
{
  "job_id": "01JZ8X...",
  "project_id": "01JZ8W...",
  "stage": "ingest",
  "attempt": 5,
  "error": {"code": "worker_lost", "message": "The worker stopped before finishing this attempt."},
  "failed_at": "2026-07-16T09:20:00Z"
}
```
