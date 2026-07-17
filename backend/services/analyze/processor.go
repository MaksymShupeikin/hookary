package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openai/openai-go/v3"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type analyzeProcessor struct {
	q       *db.Queries
	pool    *pgxpool.Pool
	storage *shared.Storage
	cfg     analyzeConfig
	client  openai.Client
}

type assetAnalysisMetadata struct {
	Transcript  *transcriptSeed `json:"transcript_seed"`
	AudioChunks []audioChunk    `json:"audio_chunks"`
}

type transcriptSeed struct {
	ObjectKey  string `json:"object_key"`
	Checksum   string `json:"checksum"`
	Source     string `json:"source"`
	Language   string `json:"language"`
	DurationMS int64  `json:"duration_ms"`
	WordCount  int    `json:"word_count"`
}

type audioChunk struct {
	ObjectKey  string `json:"object_key"`
	StartMS    int64  `json:"start_ms"`
	DurationMS int64  `json:"duration_ms"`
	SizeBytes  int64  `json:"size_bytes"`
	Checksum   string `json:"checksum"`
}

func (p *analyzeProcessor) Process(
	ctx context.Context,
	job db.Job,
	report shared.ProgressReporter,
) error {
	if job.AssetID == nil {
		return reject("asset_missing", "The analyze job has no source asset.")
	}
	existing, err := p.q.GetTranscriptByAsset(ctx, *job.AssetID)
	if err == nil {
		document, downloadErr := shared.DownloadTranscript(ctx, p.storage, existing.ObjectKey)
		if downloadErr != nil {
			return retry("transcript_unavailable", "The stored transcript is unavailable.")
		}
		if err := report(0.82); err != nil {
			return err
		}
		return p.selectAndSaveClips(ctx, job, document, report)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return retry("database_unavailable", "Could not check the existing transcript.")
	}
	asset, err := p.q.GetAssetForAnalyze(ctx, db.GetAssetForAnalyzeParams{
		ID: *job.AssetID, ProjectID: job.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return reject("asset_missing", "The source asset no longer exists.")
	}
	if err != nil {
		return retry("database_unavailable", "Could not load the source asset.")
	}
	if asset.Status != "ready" {
		return retry("asset_not_ready", "The source asset is not ready for analysis.")
	}
	var metadata assetAnalysisMetadata
	if err := json.Unmarshal(asset.Metadata, &metadata); err != nil {
		return reject("analysis_metadata_invalid", "The source analysis metadata is invalid.")
	}
	if metadata.Transcript != nil {
		document, loadErr := p.loadCaptionTranscript(ctx, *metadata.Transcript)
		if loadErr != nil {
			return loadErr
		}
		if err := p.saveTranscript(
			ctx, *job.AssetID, metadata.Transcript.ObjectKey,
			metadata.Transcript.Checksum, document,
		); err != nil {
			return retry("database_unavailable", "Could not record the source captions.")
		}
		if err := report(0.82); err != nil {
			return err
		}
		return p.selectAndSaveClips(ctx, job, document, report)
	}
	if len(metadata.AudioChunks) == 0 {
		return reject("audio_missing", "The source has no audio prepared for transcription.")
	}
	if p.cfg.APIKey == "" || p.cfg.APIKey == "CHANGE_ME" {
		return reject("ai_not_configured", "OpenAI is not configured for transcription.")
	}

	workDir, err := os.MkdirTemp(p.cfg.WorkDir, "analyze-"+job.ID+"-")
	if err != nil {
		return retry("work_storage_unavailable", "Temporary analysis storage is unavailable.")
	}
	defer os.RemoveAll(workDir)
	document, err := p.transcribeChunks(ctx, job, metadata.AudioChunks, workDir, report)
	if err != nil {
		return err
	}
	path := filepath.Join(workDir, "transcript.json.gz")
	checksum, size, err := shared.WriteTranscriptFile(path, document)
	if err != nil {
		return retry("transcript_encode_failed", "Could not encode the transcript.")
	}
	key := "work/" + job.ID + "/transcript/words.json.gz"
	if err := shared.UploadTranscript(ctx, p.storage, key, path, checksum, size); err != nil {
		return retry("transcript_upload_failed", "Could not store the transcript.")
	}
	if err := p.saveTranscript(ctx, *job.AssetID, key, checksum, document); err != nil {
		return retry("database_unavailable", "Could not record the transcript.")
	}
	return p.selectAndSaveClips(ctx, job, document, report)
}

func (p *analyzeProcessor) loadCaptionTranscript(
	ctx context.Context,
	seed transcriptSeed,
) (shared.TranscriptDocument, error) {
	document, err := shared.DownloadTranscript(ctx, p.storage, seed.ObjectKey)
	if err != nil {
		return shared.TranscriptDocument{}, retry("captions_unavailable", "Stored source captions are unavailable.")
	}
	if document.Source != "youtube_manual" && document.Source != "youtube_auto" {
		return shared.TranscriptDocument{}, reject("captions_invalid", "Stored source captions are invalid.")
	}
	return document, nil
}

func (p *analyzeProcessor) saveTranscript(
	ctx context.Context,
	assetID, key, checksum string,
	document shared.TranscriptDocument,
) error {
	language, duration := document.Language, document.DurationMS
	wordCount := int32(len(document.Words))
	_, err := p.q.UpsertTranscript(ctx, db.UpsertTranscriptParams{
		ID: shared.NewID(), AssetID: assetID, Language: &language,
		DurationMs: &duration, ObjectKey: key, Checksum: checksum,
		WordCount: &wordCount, Model: document.Source,
		Version: int32(document.Version),
	})
	return err
}

func reject(code, message string) error {
	return &shared.JobFailure{Code: code, Message: message, Retryable: false}
}

func retry(code, message string) error {
	return &shared.JobFailure{Code: code, Message: message, Retryable: true}
}
