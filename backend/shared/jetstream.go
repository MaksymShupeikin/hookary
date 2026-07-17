package shared

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamJobs = "HOOKARY_JOBS"
	StreamDLQ  = "HOOKARY_DLQ"
)

func EnsureJetStream(ctx context.Context, js jetstream.JetStream) error {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        StreamJobs,
		Description: "Durable Hookary background jobs",
		Subjects:    []string{SubjectIngest, SubjectAnalyze, SubjectRender, SubjectPublish},
		Retention:   jetstream.WorkQueuePolicy,
		MaxAge:      7 * 24 * time.Hour,
		MaxMsgSize:  maxJobMessageBytes,
		Storage:     jetstream.FileStorage,
		Replicas:    1,
		Duplicates:  10 * time.Minute,
	}); err != nil {
		return fmt.Errorf("ensure jobs stream: %w", err)
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        StreamDLQ,
		Description: "Terminal Hookary job failures",
		Subjects:    []string{SubjectDLQPrefix + "*"},
		Retention:   jetstream.LimitsPolicy,
		MaxMsgs:     10_000,
		MaxAge:      30 * 24 * time.Hour,
		MaxMsgSize:  maxJobMessageBytes,
		Storage:     jetstream.FileStorage,
		Replicas:    1,
		Duplicates:  10 * time.Minute,
	}); err != nil {
		return fmt.Errorf("ensure dlq stream: %w", err)
	}
	return nil
}

func JobSubject(stage string) (string, error) {
	switch stage {
	case "ingest":
		return SubjectIngest, nil
	case "analyze":
		return SubjectAnalyze, nil
	case "render":
		return SubjectRender, nil
	case "publish":
		return SubjectPublish, nil
	default:
		return "", fmt.Errorf("unknown job stage %q", stage)
	}
}

func StageForSubject(subject string) (string, error) {
	switch subject {
	case SubjectIngest:
		return "ingest", nil
	case SubjectAnalyze:
		return "analyze", nil
	case SubjectRender:
		return "render", nil
	case SubjectPublish:
		return "publish", nil
	default:
		return "", fmt.Errorf("unknown job subject %q", subject)
	}
}
