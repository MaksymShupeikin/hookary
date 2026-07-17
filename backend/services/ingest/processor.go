package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type ingestProcessor struct {
	q            *db.Queries
	storage      *shared.Storage
	cfg          ingestConfig
	proxyAddress string
}

type ingestResult struct {
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	Checksum    string
	Probe       mediaProbe
	URL         urlMediaMetadata
	Transcript  *storedTranscriptSeed
	AudioChunks []storedAudioChunk
}

type storedAssetMetadata struct {
	IngestVersion int                   `json:"ingest_version"`
	Format        string                `json:"format"`
	VideoCodec    string                `json:"video_codec"`
	AudioCodec    string                `json:"audio_codec"`
	Title         string                `json:"title,omitempty"`
	ExternalID    string                `json:"external_id,omitempty"`
	Extractor     string                `json:"extractor,omitempty"`
	Transcript    *storedTranscriptSeed `json:"transcript_seed,omitempty"`
	AudioChunks   []storedAudioChunk    `json:"audio_chunks,omitempty"`
}

type storedTranscriptSeed struct {
	ObjectKey  string `json:"object_key"`
	Checksum   string `json:"checksum"`
	Source     string `json:"source"`
	Language   string `json:"language,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	WordCount  int    `json:"word_count"`
}

func (p *ingestProcessor) Process(
	ctx context.Context,
	job db.Job,
	report shared.ProgressReporter,
) error {
	if job.AssetID == nil {
		return reject("asset_missing", "The ingest job has no source asset.")
	}
	asset, err := p.q.GetAssetForIngest(ctx, db.GetAssetForIngestParams{
		ID: *job.AssetID, ProjectID: job.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return reject("asset_missing", "The source asset no longer exists.")
	}
	if err != nil {
		return retry("database_unavailable", "Could not load the source asset.")
	}
	var previousMetadata storedAssetMetadata
	upgradeExisting := false
	if asset.Status == "ready" {
		if json.Unmarshal(asset.Metadata, &previousMetadata) == nil &&
			previousMetadata.IngestVersion >= 2 &&
			(previousMetadata.Transcript != nil || len(previousMetadata.AudioChunks) > 0) {
			return report(0.99)
		}
		upgradeExisting = true
	}

	workDir, err := os.MkdirTemp(p.cfg.WorkDir, "ingest-"+job.ID+"-")
	if err != nil {
		return retry("work_storage_unavailable", "Temporary ingest storage is unavailable.")
	}
	defer os.RemoveAll(workDir)

	var result ingestResult
	switch {
	case upgradeExisting:
		result, err = p.processExisting(ctx, job.ID, asset, previousMetadata, workDir, report)
	case asset.SourceKind == "r2_upload":
		result, err = p.processUpload(ctx, job.ID, asset, workDir, report)
	case asset.SourceKind == "public_url":
		result, err = p.processPublicURL(ctx, job.ID, asset, workDir, report)
	default:
		return reject("source_not_supported", "That source type is not supported.")
	}
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(storedAssetMetadata{
		IngestVersion: 2,
		Format:        result.Probe.Format, VideoCodec: result.Probe.VideoCodec,
		AudioCodec: result.Probe.AudioCodec, Title: result.URL.Title,
		ExternalID: result.URL.ID, Extractor: result.URL.Extractor,
		Transcript: result.Transcript, AudioChunks: result.AudioChunks,
	})
	if err != nil {
		return retry("metadata_failed", "Could not record source metadata.")
	}
	duration, width, height := result.Probe.DurationMS, result.Probe.Width, result.Probe.Height
	contentType, size, checksum := result.ContentType, result.SizeBytes, result.Checksum
	if _, err := p.q.CompleteIngestAsset(ctx, db.CompleteIngestAssetParams{
		ID: asset.ID, ProjectID: asset.ProjectID, ObjectKey: result.ObjectKey,
		ContentType: &contentType, SizeBytes: &size, DurationMs: &duration,
		Width: &width, Height: &height, Checksum: &checksum, Metadata: metadata,
	}); err != nil {
		return retry("database_unavailable", "Could not finish source ingest.")
	}
	return report(0.99)
}

func (p *ingestProcessor) processUpload(
	ctx context.Context,
	jobID string,
	asset db.Asset,
	workDir string,
	report shared.ProgressReporter,
) (ingestResult, error) {
	localPath := filepath.Join(workDir, "source")
	size, checksum, err := downloadObject(ctx, p.storage, asset.ObjectKey, localPath, asset.SizeBytes, p.cfg.MaxMediaBytes)
	if errors.Is(err, errMediaTooLarge) {
		return ingestResult{}, reject("media_too_large", "The source video is larger than the ingest limit.")
	}
	if err != nil {
		return ingestResult{}, retry("source_unavailable", "Could not download the uploaded source.")
	}
	if err := report(0.35); err != nil {
		return ingestResult{}, err
	}
	probe, err := probeMedia(ctx, p.cfg, localPath)
	if err != nil {
		return ingestResult{}, reject("media_invalid", err.Error())
	}
	if err := report(0.65); err != nil {
		return ingestResult{}, err
	}
	chunks, err := p.prepareAudio(ctx, jobID, localPath, workDir, probe)
	if err != nil {
		return ingestResult{}, err
	}
	if err := report(0.9); err != nil {
		return ingestResult{}, err
	}
	return ingestResult{
		ObjectKey: asset.ObjectKey, ContentType: probe.ContentType,
		SizeBytes: size, Checksum: checksum, Probe: probe, AudioChunks: chunks,
	}, nil
}

func (p *ingestProcessor) processPublicURL(
	ctx context.Context,
	jobID string,
	asset db.Asset,
	workDir string,
	report shared.ProgressReporter,
) (ingestResult, error) {
	if asset.SourceUrl == nil {
		return ingestResult{}, reject("url_missing", "The source URL is missing.")
	}
	if _, err := shared.ValidatePublicURL(ctx, *asset.SourceUrl); err != nil {
		return ingestResult{}, reject("url_rejected", "The source URL is not publicly reachable.")
	}
	metadata, err := inspectURL(ctx, p.cfg, p.proxyAddress, *asset.SourceUrl)
	if errors.Is(err, errMediaTooLarge) {
		return ingestResult{}, reject("media_too_large", "The source video is larger than the ingest limit.")
	}
	var policyError *mediaPolicyError
	if errors.As(err, &policyError) {
		return ingestResult{}, reject("url_import_rejected", policyError.Error())
	}
	if err != nil {
		return ingestResult{}, retry("url_metadata_failed", "Could not read the public video metadata.")
	}
	if err := report(0.15); err != nil {
		return ingestResult{}, err
	}
	downloaded, err := downloadURL(ctx, p.cfg, p.proxyAddress, *asset.SourceUrl, workDir)
	if errors.Is(err, errMediaTooLarge) {
		return ingestResult{}, reject("media_too_large", "The source video is larger than the ingest limit.")
	}
	if err != nil {
		return ingestResult{}, retry("url_download_failed", "Could not download the public video.")
	}
	if err := report(0.5); err != nil {
		return ingestResult{}, err
	}
	normalized, err := ensureMP4(ctx, p.cfg, downloaded, workDir)
	if err != nil {
		return ingestResult{}, reject("media_invalid", err.Error())
	}
	probe, err := probeMedia(ctx, p.cfg, normalized)
	if err != nil {
		return ingestResult{}, reject("media_invalid", err.Error())
	}
	if err := report(0.65); err != nil {
		return ingestResult{}, err
	}
	size, checksum, err := hashFile(normalized, p.cfg.MaxMediaBytes)
	if errors.Is(err, errMediaTooLarge) {
		return ingestResult{}, reject("media_too_large", "The normalized video is larger than the ingest limit.")
	}
	if err != nil {
		return ingestResult{}, retry("work_storage_unavailable", "Could not read the normalized video.")
	}
	if _, err := uploadObject(ctx, p.storage, asset.ObjectKey, normalized, "video/mp4", checksum); err != nil {
		return ingestResult{}, retry("source_upload_failed", "Could not store the imported video.")
	}
	if err := report(0.78); err != nil {
		return ingestResult{}, err
	}
	transcript := p.prepareCaptions(ctx, jobID, *asset.SourceUrl, workDir, metadata, probe)
	var chunks []storedAudioChunk
	if transcript == nil {
		chunks, err = p.prepareAudio(ctx, jobID, normalized, workDir, probe)
		if err != nil {
			return ingestResult{}, err
		}
	}
	if err := report(0.94); err != nil {
		return ingestResult{}, err
	}
	return ingestResult{
		ObjectKey: asset.ObjectKey, ContentType: "video/mp4", SizeBytes: size,
		Checksum: checksum, Probe: probe, URL: metadata,
		Transcript: transcript, AudioChunks: chunks,
	}, nil
}

func reject(code, message string) error {
	return &shared.JobFailure{Code: code, Message: message, Retryable: false}
}

func retry(code, message string) error {
	return &shared.JobFailure{Code: code, Message: message, Retryable: true}
}

func requireBinary(path string) error {
	if _, err := exec.LookPath(path); err != nil {
		return fmt.Errorf("required media binary %q: %w", path, err)
	}
	return nil
}
