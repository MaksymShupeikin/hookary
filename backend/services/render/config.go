package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type renderConfig struct {
	WorkDir       string
	FFmpegPath    string
	FFprobePath   string
	Timeout       time.Duration
	MaxSourceSize int64
	Threads       int
	MaxConcurrent int
}

func loadRenderConfig() (renderConfig, error) {
	availableCPU := runtime.GOMAXPROCS(0)
	cfg := renderConfig{
		WorkDir:       shared.EnvOr("RENDER_WORK_DIR", "/work"),
		FFmpegPath:    shared.EnvOr("FFMPEG_PATH", "ffmpeg"),
		FFprobePath:   shared.EnvOr("FFPROBE_PATH", "ffprobe"),
		Timeout:       2 * time.Hour,
		MaxSourceSize: 4 << 30,
		Threads:       max(1, availableCPU/2),
		MaxConcurrent: max(1, availableCPU/2),
	}
	var problems []error
	if raw := os.Getenv("RENDER_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			problems = append(problems, errors.New("RENDER_TIMEOUT must be a positive duration"))
		} else {
			cfg.Timeout = value
		}
	}
	if raw := os.Getenv("RENDER_MAX_SOURCE_BYTES"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			problems = append(problems, errors.New("RENDER_MAX_SOURCE_BYTES must be a positive integer"))
		} else {
			cfg.MaxSourceSize = value
		}
	}
	for key, target := range map[string]*int{
		"RENDER_FFMPEG_THREADS": &cfg.Threads,
		"RENDER_MAX_CONCURRENT": &cfg.MaxConcurrent,
	} {
		if raw := os.Getenv(key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value <= 0 {
				problems = append(problems, fmt.Errorf("%s must be a positive integer", key))
			} else {
				*target = value
			}
		}
	}
	return cfg, errors.Join(problems...)
}
