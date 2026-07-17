package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func main() {
	cfg, logger, err := shared.Bootstrap("render")
	if err != nil {
		logger.Fatal().Err(err).Msg("configuration")
	}

	ctx, stop := shared.ShutdownContext()
	defer stop()
	renderCfg, err := loadRenderConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("render configuration")
	}
	if err := os.MkdirAll(renderCfg.WorkDir, 0o700); err != nil {
		logger.Fatal().Err(err).Msg("create render work directory")
	}
	if err := cleanStaleRenderWork(renderCfg.WorkDir); err != nil {
		logger.Fatal().Err(err).Msg("clean stale render work")
	}
	for _, binary := range []string{renderCfg.FFmpegPath, renderCfg.FFprobePath} {
		if _, err := exec.LookPath(binary); err != nil {
			logger.Fatal().Err(fmt.Errorf("find %s: %w", binary, err)).Msg("media binary")
		}
	}

	deps, closeDeps, err := shared.Connect(ctx, "render", cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("connect dependencies")
	}
	defer closeDeps()

	addr := shared.EnvOr("RENDER_HEALTH_ADDR", ":8083")
	logger.Info().
		Str("addr", addr).
		Str("env", cfg.Env).
		Int("ffmpeg_threads", renderCfg.Threads).
		Int("max_concurrent", renderCfg.MaxConcurrent).
		Msg("listening")

	processor := &renderProcessor{
		q: db.New(deps.DB), storage: deps.Storage,
		cfg: renderCfg, log: logger,
		semaphore: make(chan struct{}, renderCfg.MaxConcurrent),
	}

	worker := &shared.JobWorker{
		Stage: "render", Name: shared.WorkerName("render"),
		Pool: deps.DB, JS: deps.JS, NATS: deps.NATS, Log: logger,
		Processor: processor.Process,
	}
	if err := shared.RunWorkerService(ctx, addr, deps.Ready(), worker.Run); err != nil {
		logger.Error().Err(err).Msg("shut down with error")
		os.Exit(1)
	}
	logger.Info().Msg("shut down cleanly")
}

func cleanStaleRenderWork(workDir string) error {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "render-") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(workDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
