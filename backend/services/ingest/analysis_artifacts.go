package main

import (
	"context"
	"path/filepath"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func (p *ingestProcessor) prepareCaptions(
	ctx context.Context,
	jobID, sourceURL, workDir string,
	metadata urlMediaMetadata,
	probe mediaProbe,
) *storedTranscriptSeed {
	track := selectCaptionTrack(metadata)
	if track == nil {
		return nil
	}
	captionPath, err := downloadCaptions(
		ctx, p.cfg, p.proxyAddress, sourceURL, workDir, track,
	)
	if err != nil {
		return nil
	}
	document, err := parseYouTubeCaptions(captionPath, *track, probe.DurationMS)
	if err != nil {
		return nil
	}
	path := filepath.Join(workDir, "transcript.json.gz")
	checksum, size, err := shared.WriteTranscriptFile(path, document)
	if err != nil {
		return nil
	}
	key := "work/" + jobID + "/transcript/words.json.gz"
	if err := shared.UploadTranscript(ctx, p.storage, key, path, checksum, size); err != nil {
		return nil
	}
	return &storedTranscriptSeed{
		ObjectKey: key, Checksum: checksum, Source: document.Source,
		Language: document.Language, DurationMS: document.DurationMS,
		WordCount: len(document.Words),
	}
}

func (p *ingestProcessor) prepareAudio(
	ctx context.Context,
	jobID, sourcePath, workDir string,
	probe mediaProbe,
) ([]storedAudioChunk, error) {
	if probe.AudioCodec == "" {
		return nil, reject("audio_missing", "The source contains no audio or usable captions.")
	}
	chunks, err := extractAndUploadAudio(
		ctx, p.storage, p.cfg, jobID, sourcePath, workDir, probe.DurationMS,
	)
	if err != nil {
		return nil, retry("audio_prepare_failed", "Could not prepare audio for transcription.")
	}
	return chunks, nil
}
