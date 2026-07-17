package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type mediaProbe struct {
	DurationMS  int64
	Width       int32
	Height      int32
	Format      string
	VideoCodec  string
	AudioCodec  string
	ContentType string
}

type ffprobeDocument struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Duration  string `json:"duration"`
		Width     int32  `json:"width"`
		Height    int32  `json:"height"`
	} `json:"streams"`
}

func probeMedia(ctx context.Context, cfg ingestConfig, path string) (mediaProbe, error) {
	probeContext, cancel := context.WithTimeout(ctx, cfg.ProbeTimeout)
	defer cancel()
	output, err := runCommand(probeContext, 2<<20, cfg.FFprobePath,
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		path,
	)
	if err != nil {
		return mediaProbe{}, errors.New("ffprobe could not read the video")
	}
	return parseProbe(output, cfg.MaxDuration)
}

func parseProbe(payload []byte, maxDuration time.Duration) (mediaProbe, error) {
	var document ffprobeDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return mediaProbe{}, errors.New("ffprobe returned invalid metadata")
	}
	result := mediaProbe{Format: document.Format.FormatName}
	duration := parseSeconds(document.Format.Duration)
	for _, stream := range document.Streams {
		if streamDuration := parseSeconds(stream.Duration); streamDuration > duration {
			duration = streamDuration
		}
		switch stream.CodecType {
		case "video":
			if result.VideoCodec == "" {
				result.VideoCodec = stream.CodecName
				result.Width = stream.Width
				result.Height = stream.Height
			}
		case "audio":
			if result.AudioCodec == "" {
				result.AudioCodec = stream.CodecName
			}
		}
	}
	if result.VideoCodec == "" || result.Width <= 0 || result.Height <= 0 {
		return mediaProbe{}, errors.New("source contains no usable video stream")
	}
	if result.AudioCodec == "" {
		return mediaProbe{}, errors.New("source contains no audio stream")
	}
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return mediaProbe{}, errors.New("source duration is missing")
	}
	if duration > maxDuration.Seconds() {
		return mediaProbe{}, fmt.Errorf("source is longer than %s", maxDuration)
	}
	result.DurationMS = int64(math.Round(duration * 1000))
	result.ContentType = probeContentType(result.Format)
	return result, nil
}

func parseSeconds(raw string) float64 {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return value
}

func probeContentType(format string) string {
	switch {
	case strings.Contains(format, "mp4"), strings.Contains(format, "mov"):
		return "video/mp4"
	case strings.Contains(format, "webm"):
		return "video/webm"
	case strings.Contains(format, "matroska"):
		return "video/x-matroska"
	case strings.Contains(format, "mpeg"):
		return "video/mpeg"
	default:
		return "application/octet-stream"
	}
}
