package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectCaptionTrackPrefersManualOriginalLanguage(t *testing.T) {
	metadata := urlMediaMetadata{
		OriginalLanguage: "pl",
		Subtitles: map[string][]captionFormat{
			"en": {{Extension: "json3"}},
			"pl": {{Extension: "json3"}},
		},
		AutomaticCaptions: map[string][]captionFormat{
			"pl": {{Extension: "json3"}},
		},
	}
	track := selectCaptionTrack(metadata)
	if track == nil || track.Language != "pl" || !track.Manual {
		t.Fatalf("track = %+v", track)
	}
}

func TestParseYouTubeCaptionsCreatesPreviewCues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captions.en.json3")
	payload := []byte(`{
  "events": [
    {"tStartMs": 1000, "dDurationMs": 2500, "segs": [
      {"utf8": "A useful "}, {"utf8": "opening."}
    ]}
  ]
}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	document, err := parseYouTubeCaptions(path, captionTrack{Language: "en", Manual: true}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if document.Source != "youtube_manual" || len(document.Cues) != 1 {
		t.Fatalf("document = %+v", document)
	}
	if document.Cues[0].Text != "A useful opening." {
		t.Fatalf("cue = %q", document.Cues[0].Text)
	}
}
