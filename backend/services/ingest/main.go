package main

import (
	"context"
	"os"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func main() {
	cfg, logger, err := shared.Bootstrap("ingest")
	if err != nil {
		logger.Fatal().Err(err).Msg("configuration")
	}

	ctx, stop := shared.ShutdownContext()
	defer stop()
	ingestCfg, err := loadIngestConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("ingest configuration")
	}
	if err := os.MkdirAll(ingestCfg.WorkDir, 0o700); err != nil {
		logger.Fatal().Err(err).Msg("create ingest work directory")
	}
	for _, binary := range []string{ingestCfg.YTDLPPath, ingestCfg.FFprobePath, ingestCfg.FFmpegPath} {
		if err := requireBinary(binary); err != nil {
			logger.Fatal().Err(err).Msg("media binary")
		}
	}
	proxy, err := startSafeProxy(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("start safe download proxy")
	}
	defer proxy.Close(context.Background())

	deps, closeDeps, err := shared.Connect(ctx, "ingest", cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("connect dependencies")
	}
	defer closeDeps()

	addr := shared.EnvOr("INGEST_HEALTH_ADDR", ":8081")
	logger.Info().
		Str("addr", addr).
		Str("env", cfg.Env).
		Int64("max_media_bytes", ingestCfg.MaxMediaBytes).
		Dur("max_duration", ingestCfg.MaxDuration).
		Msg("listening")

	processor := &ingestProcessor{
		q: db.New(deps.DB), storage: deps.Storage, cfg: ingestCfg,
		proxyAddress: proxy.URL(),
	}

	worker := &shared.JobWorker{
		Stage: "ingest", Name: shared.WorkerName("ingest"),
		Pool: deps.DB, JS: deps.JS, NATS: deps.NATS, Log: logger,
		Processor: processor.Process,
	}
	if err := shared.RunWorkerService(ctx, addr, deps.Ready(), worker.Run); err != nil {
		logger.Error().Err(err).Msg("shut down with error")
		os.Exit(1)
	}
	logger.Info().Msg("shut down cleanly")
}
