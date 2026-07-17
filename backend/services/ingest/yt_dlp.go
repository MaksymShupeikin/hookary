package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type urlMediaMetadata struct {
	ID                string                     `json:"id"`
	Title             string                     `json:"title"`
	Extractor         string                     `json:"extractor"`
	Language          string                     `json:"language"`
	OriginalLanguage  string                     `json:"original_language"`
	Duration          float64                    `json:"duration"`
	Filesize          float64                    `json:"filesize"`
	FilesizeApprox    float64                    `json:"filesize_approx"`
	IsLive            bool                       `json:"is_live"`
	WasLive           bool                       `json:"was_live"`
	HasDRM            bool                       `json:"_has_drm"`
	Subtitles         map[string][]captionFormat `json:"subtitles"`
	AutomaticCaptions map[string][]captionFormat `json:"automatic_captions"`
}

type mediaPolicyError struct{ message string }

func (e *mediaPolicyError) Error() string { return e.message }

func inspectURL(ctx context.Context, cfg ingestConfig, proxyAddress, sourceURL string) (urlMediaMetadata, error) {
	metadataContext, cancel := context.WithTimeout(ctx, cfg.MetadataTimeout)
	defer cancel()
	output, err := runCommand(metadataContext, 4<<20, cfg.YTDLPPath,
		"--ignore-config",
		"--no-cache-dir",
		"--js-runtimes", "node",
		"--no-playlist",
		"--no-warnings",
		"--skip-download",
		"--dump-single-json",
		"--socket-timeout", "20",
		"--proxy", proxyAddress,
		"--", sourceURL,
	)
	if err != nil {
		return urlMediaMetadata{}, errors.New("the public video metadata could not be read")
	}
	var metadata urlMediaMetadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		return urlMediaMetadata{}, errors.New("the public video metadata was invalid")
	}
	if err := validateURLMetadata(metadata, cfg); err != nil {
		return urlMediaMetadata{}, err
	}
	return metadata, nil
}

func validateURLMetadata(metadata urlMediaMetadata, cfg ingestConfig) error {
	if metadata.IsLive || metadata.WasLive {
		return &mediaPolicyError{message: "Live streams are not supported."}
	}
	if metadata.HasDRM {
		return &mediaPolicyError{message: "DRM-protected sources are not supported."}
	}
	// Generic direct-file extractors may not know duration until the file has
	// been downloaded. Unknown is allowed here; ffprobe requires and validates
	// the duration before the asset can become ready.
	if metadata.Duration > 0 && time.Duration(metadata.Duration*float64(time.Second)) > cfg.MaxDuration {
		return &mediaPolicyError{message: fmt.Sprintf("Video duration must be at most %s.", cfg.MaxDuration)}
	}
	declaredSize := max(metadata.Filesize, metadata.FilesizeApprox)
	if declaredSize > float64(cfg.MaxMediaBytes) {
		return errMediaTooLarge
	}
	return nil
}

func downloadURL(
	ctx context.Context,
	cfg ingestConfig,
	proxyAddress, sourceURL, workDir string,
) (string, error) {
	downloadContext, cancel := context.WithTimeout(ctx, cfg.DownloadTimeout)
	defer cancel()
	exceeded := make(chan struct{}, 1)
	go watchDirectorySize(downloadContext, workDir, cfg.MaxMediaBytes*2, cancel, exceeded)

	template := filepath.Join(workDir, "download.%(ext)s")
	_, err := runCommand(downloadContext, 64<<10, cfg.YTDLPPath,
		"--ignore-config",
		"--no-cache-dir",
		"--js-runtimes", "node",
		"--no-playlist",
		"--no-warnings",
		"--no-progress",
		"--socket-timeout", "20",
		"--retries", "3",
		"--fragment-retries", "3",
		"--max-filesize", strconv.FormatInt(cfg.MaxMediaBytes, 10),
		"--format", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/bv*+ba/b",
		"--merge-output-format", "mp4",
		"--output", template,
		"--proxy", proxyAddress,
		"--", sourceURL,
	)
	select {
	case <-exceeded:
		return "", errMediaTooLarge
	default:
	}
	if err != nil {
		return "", errors.New("the public video could not be downloaded")
	}
	path, err := findDownloadedFile(workDir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", errors.New("downloaded video disappeared")
	}
	if info.Size() <= 0 || info.Size() > cfg.MaxMediaBytes {
		return "", errMediaTooLarge
	}
	return path, nil
}

func findDownloadedFile(workDir string) (string, error) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return "", err
	}
	var selected string
	var selectedSize int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "download.") ||
			strings.HasSuffix(entry.Name(), ".part") || strings.HasSuffix(entry.Name(), ".ytdl") ||
			strings.HasSuffix(entry.Name(), ".json3") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Size() > selectedSize {
			selected = filepath.Join(workDir, entry.Name())
			selectedSize = info.Size()
		}
	}
	if selected == "" {
		return "", errors.New("yt-dlp produced no video file")
	}
	return selected, nil
}

func ensureMP4(ctx context.Context, cfg ingestConfig, input, workDir string) (string, error) {
	if strings.EqualFold(filepath.Ext(input), ".mp4") {
		return input, nil
	}
	output := filepath.Join(workDir, "normalized.mp4")
	normalizeContext, cancel := context.WithTimeout(ctx, cfg.NormalizeTimeout)
	defer cancel()
	if _, err := runCommand(normalizeContext, 64<<10, cfg.FFmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", input,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c", "copy", "-movflags", "+faststart",
		output,
	); err == nil {
		return output, nil
	}
	_ = os.Remove(output)
	if _, err := runCommand(normalizeContext, 64<<10, cfg.FFmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", input,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "21",
		"-c:a", "aac", "-b:a", "160k", "-movflags", "+faststart",
		output,
	); err != nil {
		return "", errors.New("video could not be normalized to MP4")
	}
	return output, nil
}

func watchDirectorySize(
	ctx context.Context,
	directory string,
	limit int64,
	cancel context.CancelFunc,
	exceeded chan<- struct{},
) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if directorySize(directory) > limit {
				select {
				case exceeded <- struct{}{}:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func directorySize(directory string) int64 {
	var total int64
	_ = filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
