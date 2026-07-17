package shared

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

// Bootstrap loads configuration and builds the logger for a process.
//
// It returns a usable logger even on failure, so a caller can report the
// configuration error through the same path as everything else instead of
// falling back to fmt.Println.
func Bootstrap(service string) (Config, zerolog.Logger, error) {
	cfg, err := Load()
	if err != nil {
		fallback := Config{Env: EnvDev, LogLevel: "info"}
		return cfg, NewLogger(service, fallback), err
	}
	return cfg, NewLogger(service, cfg), nil
}

// ShutdownContext returns a context cancelled on SIGINT or SIGTERM. That signal
// is how Docker, and later the host, ask a process to drain rather than die, so
// every long-running loop must select on it. The caller must defer stop.
func ShutdownContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// Deps are the backing services a Hookary process talks to. Every service needs
// all of them, so they are opened together rather than one connection helper at
// a time in five mains.
type Deps struct {
	DB      *pgxpool.Pool
	NATS    *nats.Conn
	JS      jetstream.JetStream
	Storage *Storage
}

// Connect opens every backing service, all or nothing: if one fails, whatever
// already opened is closed before returning, so a failed startup leaks no
// connections.
//
// The returned func releases them in reverse order and must be deferred.
func Connect(ctx context.Context, service string, cfg Config, logger zerolog.Logger) (Deps, func(), error) {
	var cleanups []func()
	unwind := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	pool, err := OpenDatabase(ctx, cfg, logger)
	if err != nil {
		unwind()
		return Deps{}, unwind, err
	}
	cleanups = append(cleanups, pool.Close)

	nc, js, err := ConnectNATS(service, cfg, logger)
	if err != nil {
		unwind()
		return Deps{}, unwind, err
	}
	cleanups = append(cleanups, func() {
		// Drain rather than Close: it finishes delivering what is already in
		// flight instead of dropping it on the floor.
		if err := nc.Drain(); err != nil {
			logger.Warn().Err(err).Msg("nats drain")
		}
	})

	storage, err := NewStorage(ctx, cfg, logger)
	if err != nil {
		unwind()
		return Deps{}, unwind, err
	}

	return Deps{DB: pool, NATS: nc, JS: js, Storage: storage}, unwind, nil
}

// Ready reports whether every backing service is usable, cheapest check first.
func (d Deps) Ready() ReadyFunc {
	return ReadyAll(
		NATSProbe(d.NATS),
		DatabaseProbe(d.DB),
		d.Storage.Probe(),
	)
}
