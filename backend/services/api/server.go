package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

// server holds what the handlers need. Handlers are methods on it, which is all
// the structure this needs: there is no interface here for a struct to hide
// behind, and no layer between a handler and its query.
type server struct {
	ctx     context.Context
	cfg     shared.Config
	log     zerolog.Logger
	pool    *pgxpool.Pool
	q       *db.Queries
	storage *shared.Storage
	google  *googleVerifier
	nats    *nats.Conn
	js      jetstream.JetStream
	broker  *jobBroker
}

func newServer(ctx context.Context, cfg shared.Config, logger zerolog.Logger, deps shared.Deps) (*server, error) {
	s := &server{
		ctx:     ctx,
		cfg:     cfg,
		log:     logger,
		pool:    deps.DB,
		q:       db.New(deps.DB),
		storage: deps.Storage,
		nats:    deps.NATS,
		js:      deps.JS,
	}
	if err := shared.EnsureJetStream(ctx, deps.JS); err != nil {
		return nil, err
	}
	broker, err := newJobBroker(ctx, deps.NATS, logger)
	if err != nil {
		return nil, err
	}
	s.broker = broker
	go shared.RunJobDispatcher(ctx, deps.DB, deps.JS, deps.NATS, logger)

	// Without Google credentials the auth endpoints answer 503 rather than the
	// process refusing to boot, so the rest of the stack is usable before
	// anyone has visited the Cloud console.
	if !cfg.Auth.GoogleConfigured() {
		logger.Warn().Msg("no GOOGLE_CLIENT_ID_* set; Google Sign-In is disabled")
		return s, nil
	}

	verifier, err := newGoogleVerifier(ctx, cfg.Auth)
	if err != nil {
		return nil, err
	}
	s.google = verifier
	return s, nil
}
