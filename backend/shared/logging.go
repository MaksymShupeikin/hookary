package shared

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// NewLogger builds the process logger.
//
// Prod writes JSON to stdout for the log pipeline to parse. Dev writes
// human-readable console output, because there the only consumer is a person
// reading `docker compose logs`.
func NewLogger(service string, cfg Config) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil || level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}

	var out io.Writer = os.Stdout
	if cfg.IsDev() {
		out = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	return zerolog.New(out).
		Level(level).
		With().
		Timestamp().
		Str("service", service).
		Logger()
}
