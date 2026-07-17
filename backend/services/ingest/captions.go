package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type captionFormat struct {
	Extension string `json:"ext"`
}

type captionTrack struct {
	Language string
	Manual   bool
}

type youtubeCaptionDocument struct {
	Events []youtubeCaptionEvent `json:"events"`
}

type youtubeCaptionEvent struct {
	StartMS    int64               `json:"tStartMs"`
	DurationMS int64               `json:"dDurationMs"`
	Segments   []youtubeCaptionSeg `json:"segs"`
}

type youtubeCaptionSeg struct {
	Text     string `json:"utf8"`
	OffsetMS int64  `json:"tOffsetMs"`
}

func selectCaptionTrack(metadata urlMediaMetadata) *captionTrack {
	preferred := []string{metadata.OriginalLanguage, metadata.Language, "en"}
	if language := selectCaptionLanguage(metadata.Subtitles, preferred); language != "" {
		return &captionTrack{Language: language, Manual: true}
	}
	if language := selectCaptionLanguage(metadata.AutomaticCaptions, preferred); language != "" {
		return &captionTrack{Language: language}
	}
	return nil
}

func selectCaptionLanguage(
	available map[string][]captionFormat,
	preferred []string,
) string {
	if len(available) == 0 {
		return ""
	}
	languages := make([]string, 0, len(available))
	for language, formats := range available {
		if len(formats) > 0 && !strings.HasPrefix(language, "live_chat") {
			languages = append(languages, language)
		}
	}
	sort.Strings(languages)
	for _, candidate := range preferred {
		if candidate == "" {
			continue
		}
		if formats := available[candidate]; len(formats) > 0 {
			return candidate
		}
		base := strings.Split(candidate, "-")[0]
		for _, language := range languages {
			if strings.Split(language, "-")[0] == base {
				return language
			}
		}
	}
	if len(languages) == 0 {
		return ""
	}
	return languages[0]
}

func downloadCaptions(
	ctx context.Context,
	cfg ingestConfig,
	proxyAddress, sourceURL, workDir string,
	track *captionTrack,
) (string, error) {
	if track == nil {
		return "", errors.New("no caption track selected")
	}
	captionContext, cancel := context.WithTimeout(ctx, cfg.MetadataTimeout)
	defer cancel()
	mode := "--write-auto-subs"
	if track.Manual {
		mode = "--write-subs"
	}
	template := filepath.Join(workDir, "captions.%(ext)s")
	_, err := runCommand(captionContext, 64<<10, cfg.YTDLPPath,
		"--ignore-config",
		"--no-cache-dir",
		"--js-runtimes", "node",
		"--no-playlist",
		"--no-warnings",
		"--skip-download",
		mode,
		"--sub-langs", track.Language,
		"--sub-format", "json3",
		"--output", template,
		"--socket-timeout", "20",
		"--proxy", proxyAddress,
		"--", sourceURL,
	)
	if err != nil {
		return "", errors.New("selected captions could not be downloaded")
	}
	entries, err := filepath.Glob(filepath.Join(workDir, "captions*.json3"))
	if err != nil || len(entries) == 0 {
		return "", errors.New("yt-dlp produced no caption file")
	}
	return entries[0], nil
}

func parseYouTubeCaptions(
	path string,
	track captionTrack,
	durationMS int64,
) (shared.TranscriptDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return shared.TranscriptDocument{}, fmt.Errorf("open captions: %w", err)
	}
	defer file.Close()
	var source youtubeCaptionDocument
	if err := json.NewDecoder(file).Decode(&source); err != nil {
		return shared.TranscriptDocument{}, fmt.Errorf("decode captions: %w", err)
	}
	words := make([]shared.TranscriptWord, 0, len(source.Events)*4)
	cues := make([]shared.TranscriptCue, 0, len(source.Events))
	for _, event := range source.Events {
		textParts := make([]string, 0, len(event.Segments))
		for index, segment := range event.Segments {
			text := strings.TrimSpace(segment.Text)
			if text == "" {
				continue
			}
			start := event.StartMS + segment.OffsetMS
			end := event.StartMS + event.DurationMS
			if index+1 < len(event.Segments) && event.Segments[index+1].OffsetMS > segment.OffsetMS {
				end = event.StartMS + event.Segments[index+1].OffsetMS
			}
			if end <= start {
				end = start + 250
			}
			words = append(words, shared.TranscriptWord{StartMS: start, EndMS: end, Text: text})
			textParts = append(textParts, text)
		}
		text := strings.Join(textParts, " ")
		if !shared.IsLikelySpeech(text) {
			continue
		}
		end := event.StartMS + event.DurationMS
		if event.DurationMS <= 0 {
			end = event.StartMS + int64(3*time.Second/time.Millisecond)
		}
		cues = append(cues, shared.TranscriptCue{
			StartMS: event.StartMS, EndMS: end, Text: text,
		})
	}
	if len(cues) == 0 {
		return shared.TranscriptDocument{}, errors.New("caption file contained no speech")
	}
	sourceName := "youtube_auto"
	if track.Manual {
		sourceName = "youtube_manual"
	}
	return shared.NewTranscriptDocument(sourceName, track.Language, durationMS, words, cues), nil
}
