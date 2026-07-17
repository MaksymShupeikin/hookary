package main

import (
	"os"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func main() {
	cfg, logger, err := shared.Bootstrap("api")
	if err != nil {
		logger.Fatal().Err(err).Msg("configuration")
	}
	if err := cfg.Auth.Validate(cfg.Env); err != nil {
		logger.Fatal().Err(err).Msg("configuration")
	}

	ctx, stop := shared.ShutdownContext()
	defer stop()

	deps, closeDeps, err := shared.Connect(ctx, "api", cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("connect dependencies")
	}
	defer closeDeps()

	srv, err := newServer(ctx, cfg, logger, deps)
	if err != nil {
		logger.Fatal().Err(err).Msg("build server")
	}

	addr := shared.EnvOr("API_ADDR", ":8080")
	logger.Info().Str("addr", addr).Str("env", cfg.Env).Msg("listening")

	if err := shared.RunHTTPServer(ctx, addr, srv.newRouter(deps.Ready())); err != nil {
		logger.Error().Err(err).Msg("shut down with error")
		os.Exit(1)
	}
	logger.Info().Msg("shut down cleanly")
}
