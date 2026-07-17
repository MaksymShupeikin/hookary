package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/openai/openai-go/v3"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type analyzeConfig struct {
	WorkDir         string
	APIKey          string
	TranscribeModel openai.AudioModel
	SelectModel     string
	PromptVersion   string
	BudgetLimitUSD  float64
	RequestTimeout  time.Duration
}

func loadAnalyzeConfig() (analyzeConfig, error) {
	cfg := analyzeConfig{
		WorkDir:         shared.EnvOr("ANALYZE_WORK_DIR", os.TempDir()),
		APIKey:          os.Getenv("OPENAI_API_KEY"),
		TranscribeModel: openai.AudioModel(shared.EnvOr("OPENAI_TRANSCRIBE_MODEL", "whisper-1")),
		SelectModel:     shared.EnvOr("OPENAI_SELECT_MODEL", "gpt-5.4-mini-2026-03-17"),
		PromptVersion:   shared.EnvOr("OPENAI_CLIP_PROMPT_VERSION", "clip-selection-v2"),
		BudgetLimitUSD:  45,
		RequestTimeout:  10 * time.Minute,
	}
	var problems []error
	if cfg.TranscribeModel != openai.AudioModelWhisper1 {
		problems = append(problems, errors.New("OPENAI_TRANSCRIBE_MODEL must be whisper-1 for word timestamps"))
	}
	if cfg.SelectModel != "gpt-5.4-mini" &&
		cfg.SelectModel != "gpt-5.4-mini-2026-03-17" {
		problems = append(problems, errors.New(
			"OPENAI_SELECT_MODEL must be gpt-5.4-mini or gpt-5.4-mini-2026-03-17",
		))
	}
	if raw := os.Getenv("OPENAI_BUDGET_LIMIT_USD"); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value <= 0 {
			problems = append(problems, errors.New("OPENAI_BUDGET_LIMIT_USD must be positive"))
		} else {
			cfg.BudgetLimitUSD = value
		}
	}
	if raw := os.Getenv("OPENAI_REQUEST_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			problems = append(problems, fmt.Errorf("OPENAI_REQUEST_TIMEOUT must be a positive duration"))
		} else {
			cfg.RequestTimeout = value
		}
	}
	return cfg, errors.Join(problems...)
}
