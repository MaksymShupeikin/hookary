package shared

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

// Subjects. Job messages carry ids only — never a video, a transcript, or any
// other payload. See contracts/events.md.
const (
	SubjectIngest  = "hookary.jobs.ingest"
	SubjectAnalyze = "hookary.jobs.analyze"
	SubjectRender  = "hookary.jobs.render"
	SubjectPublish = "hookary.jobs.publish"

	SubjectJobUpdated = "hookary.events.job-updated"

	// SubjectDLQPrefix is completed with the stage name. Nothing consumes the
	// dead-letter subjects automatically; they are parked for a human.
	SubjectDLQPrefix = "hookary.dlq."
)

// ConnectNATS opens the connection and the JetStream handle.
//
// Reconnects are unlimited on purpose: NATS restarting is a blip the workers
// should ride out, not a reason for five services to crash-loop behind it.
func ConnectNATS(service string, cfg Config, logger zerolog.Logger) (*nats.Conn, jetstream.JetStream, error) {
	opts := []nats.Option{
		nats.Name("hookary-" + service),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.Warn().Err(err).Msg("nats disconnected")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info().Str("url", nc.ConnectedUrl()).Msg("nats reconnected")
		}),
	}

	nc, err := nats.Connect(cfg.NATSURL, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("connect nats: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("open jetstream: %w", err)
	}

	logger.Info().Str("url", nc.ConnectedUrl()).Msg("nats connected")
	return nc, js, nil
}

// NATSProbe reports connection health for /health/ready.
func NATSProbe(nc *nats.Conn) ReadyFunc {
	return func(context.Context) error {
		if !nc.IsConnected() {
			return fmt.Errorf("nats: %s", nc.Status())
		}
		return nil
	}
}
