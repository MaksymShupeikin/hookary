package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindDownloadedFileIgnoresPartialFiles(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "download.webm.part"), make([]byte, 20), 0o600); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(directory, "download.mp4")
	if err := os.WriteFile(want, make([]byte, 10), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := findDownloadedFile(directory)
	if err != nil {
		t.Fatalf("findDownloadedFile: %v", err)
	}
	if got != want {
		t.Fatalf("path = %s, want %s", got, want)
	}
}

func TestURLMetadataAllowsUnknownDurationUntilFFprobe(t *testing.T) {
	cfg := ingestConfig{MaxDuration: 4 * time.Hour, MaxMediaBytes: 1 << 30}
	if err := validateURLMetadata(urlMediaMetadata{}, cfg); err != nil {
		t.Fatalf("unknown duration rejected: %v", err)
	}
	if err := validateURLMetadata(urlMediaMetadata{Duration: 5 * 60 * 60}, cfg); err == nil {
		t.Fatal("known overlong duration was accepted")
	}
}
