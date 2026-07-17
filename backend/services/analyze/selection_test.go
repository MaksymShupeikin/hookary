package main

import (
	"fmt"
	"testing"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func TestBuildCandidateWindowsOnlyEmitsSelectableDurations(t *testing.T) {
	segments := testSegments()

	windows := buildCandidateWindows(segments)

	if len(windows) != 6 {
		t.Fatalf("window count = %d, want 6: %+v", len(windows), windows)
	}
	for index, window := range windows {
		if window.DurationMS < minimumCandidateWindowMS ||
			window.DurationMS > maximumCandidateWindowMS {
			t.Fatalf("window %s has invalid duration %d", window.ID, window.DurationMS)
		}
		wantID := fmt.Sprintf("w%04d", index+1)
		if window.ID != wantID {
			t.Fatalf("window id = %q, want %q", window.ID, wantID)
		}
	}
}

func TestValidateClipSelectionsUsesWindowsAndDropsOverlap(t *testing.T) {
	document := shared.TranscriptDocument{
		DurationMS: 65_000,
		Segments:   testSegments(),
		Words: []shared.TranscriptWord{
			{StartMS: 0, EndMS: 15_000, Text: "one."},
			{StartMS: 15_000, EndMS: 30_000, Text: "two."},
			{StartMS: 30_000, EndMS: 45_000, Text: "three."},
			{StartMS: 45_000, EndMS: 60_000, Text: "four."},
		},
	}
	windows := buildCandidateWindows(document.Segments)
	selections := []modelClipSelection{
		{
			WindowID: "w0001", Score: 0.4,
			Hook: "lower", Title: "Lower", Reason: "overlaps", ContextRisk: "low",
		},
		{
			WindowID: "w0001", Score: 0.9,
			Hook: "strong", Title: "Strong", Reason: "best range", ContextRisk: "low",
		},
		{
			WindowID: "missing", Score: 0.8,
			Hook: "bad", Title: "Bad", Reason: "invalid id", ContextRisk: "low",
		},
		{
			WindowID: "w0006", Score: 0.7,
			Hook: "second", Title: "Second", Reason: "separate range", ContextRisk: "medium",
		},
	}

	clips := validateClipSelections(document, windows, selections)

	if len(clips) != 2 {
		t.Fatalf("clip count = %d, want 2: %+v", len(clips), clips)
	}
	if clips[0].Title != "Strong" || clips[0].StartMS != 0 || clips[0].EndMS != 30_000 {
		t.Fatalf("first clip = %+v", clips[0])
	}
	if clips[1].ContextRisk != "medium" || clips[1].StartMS != 30_000 {
		t.Fatalf("second clip = %+v", clips[1])
	}
}

func TestSpeechAlignedBoundsFinishesWordAtPause(t *testing.T) {
	document := shared.TranscriptDocument{
		DurationMS: 10_000,
		Words: []shared.TranscriptWord{
			{StartMS: 100, EndMS: 800, Text: "before"},
			{StartMS: 900, EndMS: 1_600, Text: "hello"},
			{StartMS: 1_600, EndMS: 2_350, Text: "world"},
			{StartMS: 2_900, EndMS: 3_500, Text: "next"},
		},
	}

	startMS, endMS := speechAlignedBounds(document, 1_000, 2_000)

	if startMS != 900 || endMS != 2_530 {
		t.Fatalf("bounds = %d..%d, want 900..2530", startMS, endMS)
	}
}

func TestSpeechAlignedBoundsUsesSentenceEndInsideTail(t *testing.T) {
	document := shared.TranscriptDocument{
		DurationMS: 10_000,
		Words: []shared.TranscriptWord{
			{StartMS: 900, EndMS: 1_600, Text: "a"},
			{StartMS: 1_600, EndMS: 2_100, Text: "complete"},
			{StartMS: 2_100, EndMS: 6_200, Text: "thought."},
			{StartMS: 6_600, EndMS: 7_100, Text: "next"},
		},
	}

	_, endMS := speechAlignedBounds(document, 1_000, 2_000)

	if endMS != 6_380 {
		t.Fatalf("end = %d, want 6380", endMS)
	}
}

func TestSpeechAlignedBoundsDoesNotPadIntoNextWord(t *testing.T) {
	document := shared.TranscriptDocument{
		DurationMS: 70_000,
		Words: []shared.TranscriptWord{
			{StartMS: 63_500, EndMS: 64_500, Text: "finished"},
			{StartMS: 64_500, EndMS: 66_000, Text: "overflow"},
		},
	}

	_, endMS := speechAlignedBounds(document, 0, 64_000)

	if endMS != 64_500 {
		t.Fatalf("end = %d, want 64500", endMS)
	}
}

func testSegments() []shared.TranscriptSegment {
	return []shared.TranscriptSegment{
		{ID: "s0001", StartMS: 0, EndMS: 15_000, Text: "one"},
		{ID: "s0002", StartMS: 15_000, EndMS: 30_000, Text: "two"},
		{ID: "s0003", StartMS: 30_000, EndMS: 45_000, Text: "three"},
		{ID: "s0004", StartMS: 45_000, EndMS: 60_000, Text: "four"},
	}
}
