package shared

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/db"
)

func describeJobFailure(err error) (string, string, bool) {
	var failure *JobFailure
	if errors.As(err, &failure) {
		return failure.Code, boundedMessage(failure.Message), failure.Retryable
	}
	return "worker_error", boundedMessage(err.Error()), true
}

func boundedMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500]
	}
	return value
}

func nextPipelineStage(stage string) string {
	switch stage {
	case "ingest":
		return "analyze"
	default:
		return ""
	}
}

// DemoProcessor proves durable delivery and the state machine without doing
// media work. Each service replaces it with its real processor in stages 7–9.
func DemoProcessor(stepDelay time.Duration) JobProcessor {
	return func(ctx context.Context, _ db.Job, report ProgressReporter) error {
		for _, progress := range []float32{0.25, 0.6, 0.9} {
			timer := time.NewTimer(stepDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			if err := report(progress); err != nil {
				return err
			}
		}
		return nil
	}
}

func ParseDemoStepDelay() time.Duration {
	delay, err := time.ParseDuration(EnvOr("DEMO_JOB_STEP_DELAY", "750ms"))
	if err != nil || delay <= 0 {
		return 750 * time.Millisecond
	}
	return delay
}
