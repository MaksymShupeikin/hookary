package main

import (
	"os"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func main() {
	cfg, logger, err := shared.Bootstrap("publish")
	if err != nil {
		logger.Fatal().Err(err).Msg("configuration")
	}

	ctx, stop := shared.ShutdownContext()
	defer stop()

	deps, closeDeps, err := shared.Connect(ctx, "publish", cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("connect dependencies")
	}
	defer closeDeps()

	addr := shared.EnvOr("PUBLISH_HEALTH_ADDR", ":8084")
	logger.Info().Str("addr", addr).Str("env", cfg.Env).Msg("listening")

	worker := &shared.JobWorker{
		Stage: "publish", Name: shared.WorkerName("publish"),
		Pool: deps.DB, JS: deps.JS, NATS: deps.NATS, Log: logger,
		Processor: shared.DemoProcessor(shared.ParseDemoStepDelay()),
	}
	if err := shared.RunWorkerService(ctx, addr, deps.Ready(), worker.Run); err != nil {
		logger.Error().Err(err).Msg("shut down with error")
		os.Exit(1)
	}
	logger.Info().Msg("shut down cleanly")
}
