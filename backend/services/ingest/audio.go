package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type storedAudioChunk struct {
	ObjectKey  string `json:"object_key"`
	StartMS    int64  `json:"start_ms"`
	DurationMS int64  `json:"duration_ms"`
	SizeBytes  int64  `json:"size_bytes"`
	Checksum   string `json:"checksum"`
}

func extractAndUploadAudio(
	ctx context.Context,
	storage *shared.Storage,
	cfg ingestConfig,
	jobID, input, workDir string,
	durationMS int64,
) ([]storedAudioChunk, error) {
	audioDir := filepath.Join(workDir, "audio")
	if err := os.Mkdir(audioDir, 0o700); err != nil {
		return nil, fmt.Errorf("create audio directory: %w", err)
	}
	audioContext, cancel := context.WithTimeout(ctx, cfg.AudioTimeout)
	defer cancel()
	pattern := filepath.Join(audioDir, "chunk-%04d.mp3")
	_, err := runCommand(audioContext, 64<<10, cfg.FFmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", input,
		"-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000",
		"-c:a", "libmp3lame", "-b:a", "32k",
		"-f", "segment",
		"-segment_time", strconv.FormatInt(int64(cfg.AudioChunkLength/time.Second), 10),
		"-reset_timestamps", "1",
		pattern,
	)
	if err != nil {
		return nil, errors.New("audio could not be prepared for transcription")
	}
	paths, err := filepath.Glob(filepath.Join(audioDir, "chunk-*.mp3"))
	if err != nil || len(paths) == 0 {
		return nil, errors.New("audio extraction produced no chunks")
	}
	sort.Strings(paths)
	chunkLengthMS := cfg.AudioChunkLength.Milliseconds()
	chunks := make([]storedAudioChunk, 0, len(paths))
	for index, path := range paths {
		startMS := int64(index) * chunkLengthMS
		chunkDurationMS := min(chunkLengthMS, max(int64(0), durationMS-startMS))
		size, checksum, err := hashFile(path, 25<<20)
		if errors.Is(err, errMediaTooLarge) {
			return nil, errors.New("an audio chunk exceeded the transcription limit")
		}
		if err != nil {
			return nil, fmt.Errorf("hash audio chunk: %w", err)
		}
		key := fmt.Sprintf("work/%s/audio/%04d.mp3", jobID, index)
		if _, err := uploadObject(ctx, storage, key, path, "audio/mpeg", checksum); err != nil {
			return nil, fmt.Errorf("upload audio chunk: %w", err)
		}
		chunks = append(chunks, storedAudioChunk{
			ObjectKey: key, StartMS: startMS, DurationMS: chunkDurationMS,
			SizeBytes: size, Checksum: checksum,
		})
	}
	return chunks, nil
}
