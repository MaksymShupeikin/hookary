package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const (
	minimumCandidateWindowMS = int64(20_000)
	maximumCandidateWindowMS = int64(60_000)
	maximumRenderedClipMS    = int64(65_000)
	maximumSpeechTailMS      = int64(5_000)
	minimumPauseMS           = int64(350)
	boundaryPaddingMS        = int64(180)
)

type candidateWindow struct {
	ID             string `json:"id"`
	StartSegmentID string `json:"start_segment_id"`
	EndSegmentID   string `json:"end_segment_id"`
	StartMS        int64  `json:"start_ms"`
	EndMS          int64  `json:"end_ms"`
	DurationMS     int64  `json:"duration_ms"`
}

type validatedClip struct {
	StartMS     int64
	EndMS       int64
	Score       float32
	Hook        string
	Title       string
	Reason      string
	ContextRisk string
}

func buildCandidateWindows(segments []shared.TranscriptSegment) []candidateWindow {
	windows := make([]candidateWindow, 0, len(segments)*2)
	for start := range segments {
		startMS := segments[start].StartMS
		for end := start; end < len(segments); end++ {
			endMS := segments[end].EndMS
			duration := endMS - startMS
			if duration > maximumCandidateWindowMS {
				break
			}
			if duration < minimumCandidateWindowMS {
				continue
			}
			windows = append(windows, candidateWindow{
				ID:             fmt.Sprintf("w%04d", len(windows)+1),
				StartSegmentID: segments[start].ID,
				EndSegmentID:   segments[end].ID,
				StartMS:        startMS,
				EndMS:          endMS,
				DurationMS:     duration,
			})
		}
	}
	return windows
}

func validateClipSelections(
	document shared.TranscriptDocument,
	windows []candidateWindow,
	selections []modelClipSelection,
) []validatedClip {
	byID := make(map[string]candidateWindow, len(windows))
	for _, window := range windows {
		byID[window.ID] = window
	}
	selections = append([]modelClipSelection(nil), selections...)
	sort.SliceStable(selections, func(i, j int) bool {
		return selections[i].Score > selections[j].Score
	})
	clips := make([]validatedClip, 0, len(selections))
	for _, selection := range selections {
		window, ok := byID[selection.WindowID]
		if !ok || selection.Score < 0 || selection.Score > 1 ||
			!validContextRisk(selection.ContextRisk) {
			continue
		}
		startMS, endMS := speechAlignedBounds(document, window.StartMS, window.EndMS)
		duration := endMS - startMS
		if duration < 15_000 || duration > maximumRenderedClipMS {
			continue
		}
		clip := validatedClip{
			StartMS: startMS, EndMS: endMS, Score: float32(selection.Score),
			Hook: boundedText(selection.Hook, 240), Title: boundedText(selection.Title, 160),
			Reason: boundedText(selection.Reason, 500), ContextRisk: selection.ContextRisk,
		}
		if clip.Hook == "" || clip.Title == "" || clip.Reason == "" ||
			overlapsAny(clip, clips) {
			continue
		}
		clips = append(clips, clip)
	}
	sort.SliceStable(clips, func(i, j int) bool { return clips[i].Score > clips[j].Score })
	if len(clips) > 5 {
		clips = clips[:5]
	}
	return clips
}

func speechAlignedBounds(
	document shared.TranscriptDocument,
	startMS, endMS int64,
) (int64, int64) {
	if len(document.Words) == 0 {
		return cueAlignedBounds(document, startMS, endMS)
	}
	startMS = alignStartToWord(document.Words, startMS)
	maximumEnd := min(document.DurationMS, startMS+maximumRenderedClipMS)
	return startMS, alignEndToSpeech(document.Words, endMS, maximumEnd)
}

func alignStartToWord(words []shared.TranscriptWord, startMS int64) int64 {
	for _, word := range words {
		if word.EndMS <= startMS {
			continue
		}
		if word.StartMS <= startMS || word.StartMS-startMS <= 500 {
			return word.StartMS
		}
		break
	}
	return startMS
}

func alignEndToSpeech(
	words []shared.TranscriptWord,
	desiredEnd, maximumEnd int64,
) int64 {
	last := -1
	for index, word := range words {
		if word.StartMS >= desiredEnd {
			break
		}
		last = index
	}
	if last < 0 {
		return min(desiredEnd+boundaryPaddingMS, maximumEnd)
	}
	// Always finish the word which contains the requested boundary. The normal
	// tail is intentionally small, but a long word must not be cut in half.
	tailLimit := min(max(desiredEnd+maximumSpeechTailMS, words[last].EndMS), maximumEnd)
	endIndex := last
	for index := last; index < len(words); index++ {
		word := words[index]
		if word.EndMS > tailLimit {
			break
		}
		endIndex = index
		if endsSpokenSentence(word.Text) {
			return paddedWordEnd(words, endIndex, maximumEnd)
		}
		if index+1 >= len(words) {
			break
		}
		next := words[index+1]
		if next.StartMS-word.EndMS >= minimumPauseMS {
			return paddedWordEnd(words, endIndex, maximumEnd)
		}
	}
	return paddedWordEnd(words, endIndex, maximumEnd)
}

func paddedWordEnd(words []shared.TranscriptWord, index int, maximumEnd int64) int64 {
	endMS := min(words[index].EndMS, maximumEnd)
	paddedEnd := min(endMS+boundaryPaddingMS, maximumEnd)
	if index+1 >= len(words) {
		return paddedEnd
	}
	nextStart := words[index+1].StartMS
	if nextStart >= endMS && nextStart < paddedEnd {
		// Padding may contain silence, but it must not start the next word and
		// then stop during its first syllable.
		return nextStart
	}
	if nextStart < endMS {
		return endMS
	}
	return paddedEnd
}

func cueAlignedBounds(
	document shared.TranscriptDocument,
	startMS, endMS int64,
) (int64, int64) {
	for _, cue := range document.Cues {
		if cue.StartMS <= startMS && cue.EndMS > startMS {
			startMS = cue.StartMS
		}
	}
	maximumEnd := min(document.DurationMS, startMS+maximumRenderedClipMS)
	for _, cue := range document.Cues {
		if cue.StartMS < endMS && cue.EndMS > endMS {
			endMS = cue.EndMS
		}
	}
	return startMS, min(endMS+boundaryPaddingMS, maximumEnd)
}

func endsSpokenSentence(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasSuffix(value, ".") || strings.HasSuffix(value, "!") ||
		strings.HasSuffix(value, "?") || strings.HasSuffix(value, "…")
}

func overlapsAny(candidate validatedClip, clips []validatedClip) bool {
	for _, existing := range clips {
		intersection := min(candidate.EndMS, existing.EndMS) - max(candidate.StartMS, existing.StartMS)
		if intersection <= 0 {
			continue
		}
		shorter := min(candidate.EndMS-candidate.StartMS, existing.EndMS-existing.StartMS)
		if float64(intersection)/float64(shorter) >= 0.5 {
			return true
		}
	}
	return false
}

func validContextRisk(value string) bool {
	return value == "low" || value == "medium" || value == "high"
}

func boundedText(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}
