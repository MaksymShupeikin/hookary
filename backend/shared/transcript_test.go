package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptBuildsReadableCuesAndStableSegments(t *testing.T) {
	document := NewTranscriptDocument("whisper-1", "en", 7000, []TranscriptWord{
		{StartMS: 0, EndMS: 800, Text: "This"},
		{StartMS: 800, EndMS: 1600, Text: "is"},
		{StartMS: 1600, EndMS: 2400, Text: "useful."},
		{StartMS: 3000, EndMS: 3900, Text: "Keep"},
		{StartMS: 3900, EndMS: 4800, Text: "watching."},
	}, nil)

	if len(document.Cues) != 2 {
		t.Fatalf("cue count = %d, want 2", len(document.Cues))
	}
	if document.Cues[0].Text != "This is useful." {
		t.Fatalf("first cue = %q", document.Cues[0].Text)
	}
	if len(document.Segments) != 1 || document.Segments[0].ID != "s0001" {
		t.Fatalf("segments = %+v", document.Segments)
	}
}

func TestTranscriptGzipRoundTrip(t *testing.T) {
	document := NewTranscriptDocument("youtube_manual", "pl", 3000, nil, []TranscriptCue{
		{StartMS: 100, EndMS: 2500, Text: "Czytelny napis."},
	})
	path := filepath.Join(t.TempDir(), "transcript.json.gz")
	checksum, size, err := WriteTranscriptFile(path, document)
	if err != nil {
		t.Fatal(err)
	}
	if len(checksum) != 64 || size <= 0 {
		t.Fatalf("checksum=%q size=%d", checksum, size)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoded, err := ReadTranscript(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(decoded.Text, "Czytelny") || len(decoded.Cues) != 1 {
		t.Fatalf("decoded = %+v", decoded)
	}
}
