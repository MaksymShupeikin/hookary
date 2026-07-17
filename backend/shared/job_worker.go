package shared

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/db"
)

type ProgressReporter func(float32) error
type JobProcessor func(context.Context, db.Job, ProgressReporter) error

type JobFailure struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *JobFailure) Error() string { return e.Message }

type JobWorker struct {
	Stage     string
	Name      string
	Pool      *pgxpool.Pool
	JS        jetstream.JetStream
	NATS      *nats.Conn
	Log       zerolog.Logger
	Processor JobProcessor

	ctx context.Context
}

func (w *JobWorker) Run(ctx context.Context) error {
	if w.Processor == nil {
		return errors.New("job worker processor is required")
	}
	subject, err := JobSubject(w.Stage)
	if err != nil {
		return err
	}
	if err := EnsureJetStream(ctx, w.JS); err != nil {
		return err
	}
	consumerName := "hookary-" + w.Stage
	consumer, err := w.JS.CreateOrUpdateConsumer(ctx, StreamJobs, jetstream.ConsumerConfig{
		Name: consumerName, Durable: consumerName,
		Description:   "Hookary " + w.Stage + " worker",
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       jobLeaseTimeout,
		MaxDeliver:    20,
		MaxAckPending: 1,
	})
	if err != nil {
		return fmt.Errorf("ensure %s consumer: %w", w.Stage, err)
	}

	w.ctx = ctx
	consume, err := consumer.Consume(w.handleMessage,
		jetstream.PullMaxMessages(1),
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			if ctx.Err() == nil {
				w.Log.Error().Err(err).Str("stage", w.Stage).Msg("consume jobs")
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("consume %s jobs: %w", w.Stage, err)
	}
	defer consume.Stop()

	w.Log.Info().Str("stage", w.Stage).Str("consumer", consumerName).Msg("job worker ready")
	<-ctx.Done()
	consume.Drain()
	return nil
}

func (w *JobWorker) handleMessage(message jetstream.Msg) {
	ctx := w.ctx
	jobMessage, err := DecodeJobMessage(message.Data())
	if err != nil {
		w.rejectMalformed(ctx, message, err)
		return
	}
	if err := jobMessage.Validate(); err != nil {
		w.rejectMalformed(ctx, message, err)
		return
	}
	stage, err := StageForSubject(message.Subject())
	if err != nil || stage != w.Stage {
		w.rejectMalformed(ctx, message, errors.New("message arrived on the wrong stage subject"))
		return
	}

	job, claimed, err := w.claim(ctx, jobMessage)
	if err != nil {
		w.Log.Error().Err(err).Str("job_id", jobMessage.JobID).Msg("claim job")
		_ = message.NakWithDelay(time.Second)
		return
	}
	if !claimed {
		w.handleUnclaimed(ctx, message, jobMessage, job)
		return
	}

	PublishJobEvent(w.NATS, job, w.Log)
	processCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go w.heartbeat(processCtx, message, job, heartbeatDone)
	report := func(progress float32) error {
		return w.reportProgress(processCtx, job, progress)
	}
	processErr := w.Processor(processCtx, job, report)
	cancel()
	<-heartbeatDone

	if ctx.Err() != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	if processErr != nil {
		w.finishFailure(ctx, message, job, processErr)
		return
	}
	w.finishSuccess(ctx, message, job)
}

func (w *JobWorker) claim(
	ctx context.Context,
	message JobMessage,
) (db.Job, bool, error) {
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return db.Job{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	job, err := q.ClaimJob(ctx, db.ClaimJobParams{
		JobID: message.JobID, Stage: w.Stage, Attempt: message.Attempt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		current, getErr := q.GetJob(ctx, message.JobID)
		if errors.Is(getErr, pgx.ErrNoRows) {
			return db.Job{}, false, nil
		}
		return current, false, getErr
	}
	if err != nil {
		return db.Job{}, false, err
	}
	workerName := w.Name
	if _, err := q.CreateJobAttempt(ctx, db.CreateJobAttemptParams{
		ID: NewID(), JobID: job.ID, Attempt: job.Attempt, Worker: &workerName,
	}); err != nil {
		return db.Job{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Job{}, false, err
	}
	return job, true, nil
}

func (w *JobWorker) handleUnclaimed(
	ctx context.Context,
	message jetstream.Msg,
	jobMessage JobMessage,
	job db.Job,
) {
	if job.ID == "" {
		// A project can be deleted while an old message is still in the stream.
		_ = ackJobMessage(message)
		return
	}
	switch {
	case job.ProjectID != jobMessage.ProjectID || job.Stage != w.Stage:
		w.rejectMalformed(ctx, message, errors.New("message ids do not match the job row"))
	case job.Status == "succeeded":
		_ = ackJobMessage(message)
	case job.Status == "failed":
		if err := PublishDeadLetter(context.WithoutCancel(ctx), w.JS, deadLetterFor(job)); err != nil {
			_ = message.NakWithDelay(time.Second)
			return
		}
		_ = ackJobMessage(message)
	case job.Status == "running":
		_ = message.NakWithDelay(2 * time.Second)
	case job.Status == "queued" && jobMessage.Attempt <= job.Attempt:
		_ = ackJobMessage(message)
	default:
		w.rejectMalformed(ctx, message, errors.New("message attempt does not match the job row"))
	}
}

func (w *JobWorker) reportProgress(ctx context.Context, job db.Job, value float32) error {
	value = max(0, min(value, 0.99))
	updated, err := db.New(w.Pool).UpdateJobProgress(ctx, db.UpdateJobProgressParams{
		ID: job.ID, Attempt: job.Attempt, Progress: &value,
	})
	if err != nil {
		return err
	}
	PublishJobEvent(w.NATS, updated, w.Log)
	return nil
}

func (w *JobWorker) heartbeat(
	ctx context.Context,
	message jetstream.Msg,
	job db.Job,
	done chan<- struct{},
) {
	defer close(done)
	ticker := time.NewTicker(jobLeaseTimeout / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = message.InProgress()
			_, err := db.New(w.Pool).TouchRunningJob(ctx, db.TouchRunningJobParams{
				ID: job.ID, Attempt: job.Attempt,
			})
			if err != nil && ctx.Err() == nil {
				w.Log.Error().Err(err).Str("job_id", job.ID).Msg("touch running job")
			}
		}
	}
}

func (w *JobWorker) finishSuccess(ctx context.Context, message jetstream.Msg, current db.Job) {
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	job, err := q.SucceedJob(ctx, db.SucceedJobParams{ID: current.ID, Attempt: current.Attempt})
	if err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	if _, err := q.SucceedJobAttempt(ctx, db.SucceedJobAttemptParams{
		JobID: current.ID, Attempt: current.Attempt,
	}); err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}

	var nextJob *db.Job
	if nextStage := nextPipelineStage(current.Stage); nextStage != "" {
		keyID := current.ID
		if current.AssetID != nil {
			keyID = *current.AssetID
		}
		idempotencyKey := nextStage + ":v1:" + keyID
		created, createErr := q.CreateNextStageJob(ctx, db.CreateNextStageJobParams{
			ID: NewID(), ProjectID: current.ProjectID, AssetID: current.AssetID,
			Stage: nextStage, IdempotencyKey: &idempotencyKey,
		})
		if createErr != nil {
			_ = message.NakWithDelay(time.Second)
			return
		}
		nextJob = &created
	} else if err := q.SetProjectReady(ctx, current.ProjectID); err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	PublishJobEvent(w.NATS, job, w.Log)
	if nextJob != nil {
		PublishJobEvent(w.NATS, *nextJob, w.Log)
	}
	_ = ackJobMessage(message)
}

func (w *JobWorker) finishFailure(
	ctx context.Context,
	message jetstream.Msg,
	current db.Job,
	processErr error,
) {
	code, description, retryable := describeJobFailure(processErr)
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, err := q.FailJobAttempt(ctx, db.FailJobAttemptParams{
		JobID: current.ID, Attempt: current.Attempt,
		ErrorCode: &code, ErrorMessage: &description,
	}); err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}

	var job db.Job
	if retryable && current.Attempt < current.MaxAttempts {
		next := time.Now().Add(RetryDelay(current.Attempt))
		job, err = q.RequeueJobAfterFailure(ctx, db.RequeueJobAfterFailureParams{
			ID: current.ID, Attempt: current.Attempt,
			NextAttemptAt: pgtype.Timestamptz{Time: next, Valid: true},
			ErrorCode:     &code, ErrorMessage: &description,
		})
	} else {
		job, err = q.FailJob(ctx, db.FailJobParams{
			ID: current.ID, Attempt: current.Attempt,
			ErrorCode: &code, ErrorMessage: &description,
		})
		if err == nil && current.Stage == "ingest" && current.AssetID != nil {
			_, err = q.FailIngestAsset(ctx, db.FailIngestAssetParams{
				ID: *current.AssetID, ProjectID: current.ProjectID,
			})
		}
		if err == nil && current.Stage == "render" && current.ClipID != nil {
			_, err = q.FailClipRender(ctx, *current.ClipID)
		}
		if err == nil {
			err = q.SetProjectFailed(ctx, current.ProjectID)
		}
	}
	if err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	PublishJobEvent(w.NATS, job, w.Log)
	if job.Status == "failed" {
		if err := PublishDeadLetter(ctx, w.JS, deadLetterFor(job)); err != nil {
			_ = message.NakWithDelay(time.Second)
			return
		}
	}
	_ = ackJobMessage(message)
}

func (w *JobWorker) rejectMalformed(ctx context.Context, message jetstream.Msg, cause error) {
	stage, _ := StageForSubject(message.Subject())
	dead := DeadLetterMessage{
		Stage:    stage,
		Error:    JobEventError{Code: "malformed_message", Message: boundedMessage(cause.Error())},
		FailedAt: time.Now().UTC(),
	}
	if err := PublishDeadLetter(context.WithoutCancel(ctx), w.JS, dead); err != nil {
		_ = message.NakWithDelay(time.Second)
		return
	}
	_ = message.Term()
}

func ackJobMessage(message jetstream.Msg) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return message.DoubleAck(ctx)
}
