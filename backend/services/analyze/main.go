package main

import (
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func main() {
	cfg, logger, err := shared.Bootstrap("analyze")
	if err != nil {
		logger.Fatal().Err(err).Msg("configuration")
	}

	ctx, stop := shared.ShutdownContext()
	defer stop()
	analyzeCfg, err := loadAnalyzeConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("analyze configuration")
	}

	deps, closeDeps, err := shared.Connect(ctx, "analyze", cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("connect dependencies")
	}
	defer closeDeps()

	addr := shared.EnvOr("ANALYZE_HEALTH_ADDR", ":8082")
	logger.Info().
		Str("addr", addr).
		Str("env", cfg.Env).
		Str("transcribe_model", string(analyzeCfg.TranscribeModel)).
		Str("select_model", analyzeCfg.SelectModel).
		Str("prompt_version", analyzeCfg.PromptVersion).
		Float64("budget_limit_usd", analyzeCfg.BudgetLimitUSD).
		Bool("openai_configured", analyzeCfg.APIKey != "" && analyzeCfg.APIKey != "CHANGE_ME").
		Msg("listening")

	client := openai.NewClient(option.WithAPIKey(analyzeCfg.APIKey))
	processor := &analyzeProcessor{
		q: db.New(deps.DB), pool: deps.DB, storage: deps.Storage,
		cfg: analyzeCfg, client: client,
	}

	worker := &shared.JobWorker{
		Stage: "analyze", Name: shared.WorkerName("analyze"),
		Pool: deps.DB, JS: deps.JS, NATS: deps.NATS, Log: logger,
		Processor: processor.Process,
	}
	if err := shared.RunWorkerService(ctx, addr, deps.Ready(), worker.Run); err != nil {
		logger.Error().Err(err).Msg("shut down with error")
		os.Exit(1)
	}
	logger.Info().Msg("shut down cleanly")
}
