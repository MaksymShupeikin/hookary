package shared

import (
	"context"
	"encoding/json"
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

const (
	dispatchInterval  = 250 * time.Millisecond
	staleScanInterval = 5 * time.Second
	jobLeaseTimeout   = 30 * time.Second
)

func RunJobDispatcher(
	ctx context.Context,
	pool *pgxpool.Pool,
	js jetstream.JetStream,
	nc *nats.Conn,
	logger zerolog.Logger,
) {
	q := db.New(pool)
	dispatchTicker := time.NewTicker(dispatchInterval)
	staleTicker := time.NewTicker(staleScanInterval)
	defer dispatchTicker.Stop()
	defer staleTicker.Stop()

	dispatchDueJobs(ctx, q, js, nc, logger)
	recoverStaleJobs(ctx, pool, js, nc, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-dispatchTicker.C:
			dispatchDueJobs(ctx, q, js, nc, logger)
		case <-staleTicker.C:
			recoverStaleJobs(ctx, pool, js, nc, logger)
		}
	}
}

func dispatchDueJobs(
	ctx context.Context,
	q *db.Queries,
	js jetstream.JetStream,
	nc *nats.Conn,
	logger zerolog.Logger,
) {
	jobs, err := q.ListDispatchableJobs(ctx, 100)
	if err != nil {
		if ctx.Err() == nil {
			logger.Error().Err(err).Msg("list dispatchable jobs")
		}
		return
	}
	for _, job := range jobs {
		if err := PublishJob(ctx, js, job); err != nil {
			logger.Error().Err(err).Str("job_id", job.ID).Msg("dispatch job")
			continue
		}
		PublishJobEvent(nc, job, logger)
	}
}

func PublishJob(ctx context.Context, js jetstream.JetStream, job db.Job) error {
	subject, err := JobSubject(job.Stage)
	if err != nil {
		return err
	}
	message := NewJobMessage(job)
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode job message: %w", err)
	}
	messageID := fmt.Sprintf("%s:%d", job.ID, message.Attempt)
	if _, err := js.Publish(ctx, subject, payload,
		jetstream.WithMsgID(messageID),
		jetstream.WithExpectStream(StreamJobs)); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

func PublishJobEvent(nc *nats.Conn, job db.Job, logger zerolog.Logger) {
	if nc == nil {
		return
	}
	payload, err := json.Marshal(NewJobUpdatedEvent(job))
	if err != nil {
		logger.Error().Err(err).Str("job_id", job.ID).Msg("encode job event")
		return
	}
	if err := nc.Publish(SubjectJobUpdated, payload); err != nil {
		logger.Error().Err(err).Str("job_id", job.ID).Msg("publish job event")
	}
}

func PublishDeadLetter(
	ctx context.Context,
	js jetstream.JetStream,
	message DeadLetterMessage,
) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode dead letter: %w", err)
	}
	messageID := fmt.Sprintf("dlq:%s:%d:%s", message.JobID, message.Attempt, message.Error.Code)
	if message.JobID == "" {
		messageID = fmt.Sprintf("dlq:%s:%d", message.Stage, message.FailedAt.UnixNano())
	}
	if _, err := js.Publish(ctx, SubjectDLQPrefix+message.Stage, payload,
		jetstream.WithMsgID(messageID),
		jetstream.WithExpectStream(StreamDLQ)); err != nil {
		return fmt.Errorf("publish dead letter: %w", err)
	}
	return nil
}

func recoverStaleJobs(
	ctx context.Context,
	pool *pgxpool.Pool,
	js jetstream.JetStream,
	nc *nats.Conn,
	logger zerolog.Logger,
) {
	cutoff := time.Now().Add(-jobLeaseTimeout)
	jobs, err := db.New(pool).ListStaleJobs(ctx, db.ListStaleJobsParams{
		UpdatedAt: pgtype.Timestamptz{Time: cutoff, Valid: true}, Limit: 100,
	})
	if err != nil {
		if ctx.Err() == nil {
			logger.Error().Err(err).Msg("list stale jobs")
		}
		return
	}
	for _, candidate := range jobs {
		job, recovered, err := recoverStaleJob(ctx, pool, candidate.ID, cutoff)
		if err != nil {
			logger.Error().Err(err).Str("job_id", candidate.ID).Msg("recover stale job")
			continue
		}
		if !recovered {
			continue
		}
		PublishJobEvent(nc, job, logger)
		if job.Status == "failed" {
			if err := PublishDeadLetter(ctx, js, deadLetterFor(job)); err != nil {
				logger.Error().Err(err).Str("job_id", job.ID).Msg("dead-letter stale job")
			}
		}
	}
}

func recoverStaleJob(
	ctx context.Context,
	pool *pgxpool.Pool,
	jobID string,
	cutoff time.Time,
) (db.Job, bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return db.Job{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	job, err := q.GetJobForUpdate(ctx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Job{}, false, nil
	}
	if err != nil {
		return db.Job{}, false, err
	}
	if job.Status != "running" || !job.UpdatedAt.Time.Before(cutoff) {
		return db.Job{}, false, nil
	}

	code := "worker_lost"
	message := "The worker stopped before finishing this attempt."
	if _, err := q.FailJobAttempt(ctx, db.FailJobAttemptParams{
		JobID: job.ID, Attempt: job.Attempt,
		ErrorCode: &code, ErrorMessage: &message,
	}); err != nil {
		return db.Job{}, false, err
	}
	if job.Attempt < job.MaxAttempts {
		next := time.Now().Add(RetryDelay(job.Attempt))
		job, err = q.RequeueJobAfterFailure(ctx, db.RequeueJobAfterFailureParams{
			ID: job.ID, Attempt: job.Attempt,
			NextAttemptAt: pgtype.Timestamptz{Time: next, Valid: true},
			ErrorCode:     &code, ErrorMessage: &message,
		})
	} else {
		job, err = q.FailJob(ctx, db.FailJobParams{
			ID: job.ID, Attempt: job.Attempt,
			ErrorCode: &code, ErrorMessage: &message,
		})
		if err == nil {
			err = q.SetProjectFailed(ctx, job.ProjectID)
		}
	}
	if err != nil {
		return db.Job{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Job{}, false, err
	}
	return job, true, nil
}

func RetryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 6)
	return min(delay, time.Minute)
}

func deadLetterFor(job db.Job) DeadLetterMessage {
	code := "job_failed"
	message := "The job exhausted its attempts."
	if job.ErrorCode != nil {
		code = *job.ErrorCode
	}
	if job.ErrorMessage != nil {
		message = *job.ErrorMessage
	}
	return DeadLetterMessage{
		JobID: job.ID, ProjectID: job.ProjectID, Stage: job.Stage,
		Attempt:  job.Attempt,
		Error:    JobEventError{Code: code, Message: message},
		FailedAt: time.Now().UTC(),
	}
}
