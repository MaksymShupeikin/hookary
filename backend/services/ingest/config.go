package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type ingestConfig struct {
	WorkDir          string
	YTDLPPath        string
	FFprobePath      string
	FFmpegPath       string
	MaxMediaBytes    int64
	MaxDuration      time.Duration
	MetadataTimeout  time.Duration
	DownloadTimeout  time.Duration
	ProbeTimeout     time.Duration
	NormalizeTimeout time.Duration
	AudioTimeout     time.Duration
	AudioChunkLength time.Duration
}

func loadIngestConfig() (ingestConfig, error) {
	cfg := ingestConfig{
		WorkDir:          shared.EnvOr("INGEST_WORK_DIR", "/work"),
		YTDLPPath:        shared.EnvOr("YT_DLP_PATH", "yt-dlp"),
		FFprobePath:      shared.EnvOr("FFPROBE_PATH", "ffprobe"),
		FFmpegPath:       shared.EnvOr("FFMPEG_PATH", "ffmpeg"),
		MaxMediaBytes:    4 << 30,
		MaxDuration:      4 * time.Hour,
		MetadataTimeout:  2 * time.Minute,
		DownloadTimeout:  90 * time.Minute,
		ProbeTimeout:     2 * time.Minute,
		NormalizeTimeout: 2 * time.Hour,
		AudioTimeout:     2 * time.Hour,
		AudioChunkLength: 20 * time.Minute,
	}

	var problems []error
	if raw := os.Getenv("INGEST_MAX_MEDIA_BYTES"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			problems = append(problems, fmt.Errorf("INGEST_MAX_MEDIA_BYTES must be a positive integer"))
		} else {
			cfg.MaxMediaBytes = value
		}
	}
	for key, target := range map[string]*time.Duration{
		"INGEST_MAX_DURATION":       &cfg.MaxDuration,
		"INGEST_METADATA_TIMEOUT":   &cfg.MetadataTimeout,
		"INGEST_DOWNLOAD_TIMEOUT":   &cfg.DownloadTimeout,
		"INGEST_PROBE_TIMEOUT":      &cfg.ProbeTimeout,
		"INGEST_NORMALIZE_TIMEOUT":  &cfg.NormalizeTimeout,
		"INGEST_AUDIO_TIMEOUT":      &cfg.AudioTimeout,
		"INGEST_AUDIO_CHUNK_LENGTH": &cfg.AudioChunkLength,
	} {
		if raw := os.Getenv(key); raw != "" {
			value, err := time.ParseDuration(raw)
			if err != nil || value <= 0 {
				problems = append(problems, fmt.Errorf("%s must be a positive duration", key))
			} else {
				*target = value
			}
		}
	}
	if err := errors.Join(problems...); err != nil {
		return ingestConfig{}, err
	}
	return cfg, nil
}
