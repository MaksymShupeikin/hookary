package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func TestWriteClipSubtitlesUsesClipRelativeTiming(t *testing.T) {
	document := shared.NewTranscriptDocument(
		"whisper-1",
		"en",
		60_000,
		nil,
		[]shared.TranscriptCue{
			{StartMS: 9_000, EndMS: 12_500, Text: "Before and inside"},
			{StartMS: 15_000, EndMS: 18_250, Text: `Use {timed} \ subtitles`},
			{StartMS: 31_000, EndMS: 34_000, Text: "After"},
		},
	)
	path := filepath.Join(t.TempDir(), "subtitles.ass")
	count, err := writeClipSubtitles(path, document, 10_000, 30_000)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("subtitle count = %d, want 2", count)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{
		"Dialogue: 0,0:00:00.00,0:00:02.50",
		"Dialogue: 0,0:00:05.00,0:00:08.25",
		`Use \{timed\} \\ subtitles`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("ASS output does not contain %q\n%s", expected, text)
		}
	}
	if strings.Contains(text, "After") {
		t.Fatal("subtitle outside the selected clip was included")
	}
}

func TestRenderFiltersProduceShortsCanvas(t *testing.T) {
	filter, complex := renderVideoFilter(db.Clip{
		CropMode: "center", FocusX: 0.5, FocusY: 0.5,
	})
	for _, expected := range []string{
		"force_original_aspect_ratio=increase",
		"gblur=sigma=42:steps=2",
		"eq=brightness=-0.30:saturation=1.20",
		"vignette=PI/4",
		"scale=1080:608:force_original_aspect_ratio=decrease",
		"pad=1080:608:(ow-iw)/2:(oh-ih)/2:color=black",
		"subtitles=filename=subtitles.ass",
	} {
		if !strings.Contains(filter, expected) {
			t.Fatalf("ambient filter does not contain %q: %s", expected, filter)
		}
	}
	if !complex || !strings.HasSuffix(filter, "[video]") {
		t.Fatalf("unexpected ambient filter: %s", filter)
	}
	if strings.Contains(filter, "pad=1040:592") {
		t.Fatalf("full-width video still has an outer frame: %s", filter)
	}
}

func TestSubtitleStyleUsesLowerShortsSafeZone(t *testing.T) {
	if !strings.Contains(assHeader, ",2,70,70,330,1") {
		t.Fatalf("subtitle style is outside the lower safe zone: %s", assHeader)
	}
}

func TestASSTimestamp(t *testing.T) {
	if got := assTimestamp(3_723_450); got != "1:02:03.45" {
		t.Fatalf("timestamp = %s", got)
	}
}

func TestCleanStaleRenderWorkLeavesUnrelatedFiles(t *testing.T) {
	workDir := t.TempDir()
	stale := filepath.Join(workDir, "render-old-job")
	keep := filepath.Join(workDir, "keep.txt")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanStaleRenderWork(workDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale work still exists: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated work was removed: %v", err)
	}
}
