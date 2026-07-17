package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type renderProcessor struct {
	q         *db.Queries
	storage   *shared.Storage
	cfg       renderConfig
	log       zerolog.Logger
	semaphore chan struct{}
}

func (p *renderProcessor) Process(
	ctx context.Context,
	job db.Job,
	report shared.ProgressReporter,
) error {
	select {
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if job.ClipID == nil || job.AssetID == nil {
		return renderReject("clip_missing", "The render job has no selected clip.")
	}
	clip, err := p.q.GetClipForRender(ctx, db.GetClipForRenderParams{
		ID: *job.ClipID, ProjectID: job.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return renderReject("clip_missing", "The selected clip no longer exists.")
	}
	if err != nil {
		return renderRetry("database_unavailable", "Could not load the selected clip.")
	}
	if clip.AssetID != *job.AssetID {
		return renderReject("clip_mismatch", "The selected clip does not match its source video.")
	}
	if clip.Status == "rendered" && clip.OutputKey != nil && p.objectExists(ctx, *clip.OutputKey) {
		return report(0.99)
	}
	if _, err := p.q.MarkClipRendering(ctx, clip.ID); err != nil {
		return renderRetry("database_unavailable", "Could not start the clip render.")
	}
	asset, err := p.q.GetAssetForAnalyze(ctx, db.GetAssetForAnalyzeParams{
		ID: clip.AssetID, ProjectID: clip.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return renderReject("asset_missing", "The source video no longer exists.")
	}
	if err != nil || asset.Status != "ready" || asset.SizeBytes == nil {
		return renderRetry("asset_unavailable", "The source video is not ready for rendering.")
	}
	transcript, err := p.q.GetTranscriptByAsset(ctx, asset.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return renderReject("transcript_missing", "The source has no subtitles to render.")
	}
	if err != nil {
		return renderRetry("database_unavailable", "Could not load the source transcript.")
	}
	if err := report(0.05); err != nil {
		return err
	}

	workDir, err := os.MkdirTemp(p.cfg.WorkDir, "render-"+job.ID+"-")
	if err != nil {
		return renderRetry("work_storage_unavailable", "Temporary render storage is unavailable.")
	}
	defer os.RemoveAll(workDir)
	if err := downloadRenderSource(
		ctx, p.storage, asset.ObjectKey, filepath.Join(workDir, "source.mp4"),
		*asset.SizeBytes, p.cfg.MaxSourceSize,
	); err != nil {
		return renderRetry("source_download_failed", "Could not download the source video for rendering.")
	}
	document, err := shared.DownloadTranscript(ctx, p.storage, transcript.ObjectKey)
	if err != nil {
		return renderRetry("transcript_download_failed", "Could not download subtitles for rendering.")
	}
	count, err := writeClipSubtitles(
		filepath.Join(workDir, "subtitles.ass"), document, clip.StartMs, clip.EndMs,
	)
	if err != nil {
		return renderRetry("subtitle_write_failed", "Could not prepare timed subtitles.")
	}
	if count == 0 {
		return renderReject("clip_has_no_speech", "The selected clip contains no timed speech.")
	}
	if err := report(0.18); err != nil {
		return err
	}

	renderContext, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	if err := runVerticalRender(renderContext, p.cfg, workDir, clip); err != nil {
		p.log.Error().Err(err).Str("job_id", job.ID).Str("clip_id", clip.ID).Msg("ffmpeg render")
		return renderRetry("ffmpeg_failed", "FFmpeg could not render the vertical clip.")
	}
	if err := validateRenderedOutput(
		renderContext, p.cfg, workDir, clip.EndMs-clip.StartMs,
	); err != nil {
		p.log.Error().Err(err).Str("job_id", job.ID).Str("clip_id", clip.ID).Msg("validate render")
		return renderRetry("render_invalid", "The rendered video failed validation.")
	}
	if err := createThumbnail(renderContext, p.cfg, workDir); err != nil {
		return renderRetry("thumbnail_failed", "Could not create the clip thumbnail.")
	}
	if err := report(0.86); err != nil {
		return err
	}

	ownerID, err := p.q.GetProjectOwnerID(ctx, clip.ProjectID)
	if err != nil {
		return renderRetry("database_unavailable", "Could not resolve the clip owner.")
	}
	baseKey := fmt.Sprintf("output/%s/%s/%s", ownerID, clip.ProjectID, clip.ID)
	outputKey := baseKey + "/v4.mp4"
	thumbnailKey := baseKey + "/thumbnail.webp"
	if err := uploadRenderObject(
		ctx, p.storage, outputKey, filepath.Join(workDir, "output.mp4"), "video/mp4",
	); err != nil {
		return renderRetry("output_upload_failed", "Could not store the rendered video.")
	}
	if err := uploadRenderObject(
		ctx, p.storage, thumbnailKey, filepath.Join(workDir, "thumbnail.webp"), "image/webp",
	); err != nil {
		return renderRetry("thumbnail_upload_failed", "Could not store the clip thumbnail.")
	}
	if _, err := p.q.CompleteClipRender(ctx, db.CompleteClipRenderParams{
		ID: clip.ID, OutputKey: &outputKey, ThumbnailKey: &thumbnailKey,
	}); err != nil {
		return renderRetry("database_unavailable", "Could not record the rendered clip.")
	}
	if clip.OutputKey != nil && *clip.OutputKey != outputKey {
		if err := p.deleteSupersededOutput(ctx, *clip.OutputKey); err != nil {
			p.log.Warn().Err(err).Str("object_key", *clip.OutputKey).
				Msg("could not remove superseded render")
		}
	}
	return report(0.99)
}

func (p *renderProcessor) objectExists(ctx context.Context, key string) bool {
	_, err := p.storage.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(p.storage.Bucket), Key: aws.String(key),
	})
	return err == nil
}

func (p *renderProcessor) deleteSupersededOutput(ctx context.Context, key string) error {
	_, err := p.storage.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(p.storage.Bucket), Key: aws.String(key),
	})
	return err
}

func renderReject(code, message string) error {
	return &shared.JobFailure{Code: code, Message: message, Retryable: false}
}

func renderRetry(code, message string) error {
	return &shared.JobFailure{Code: code, Message: message, Retryable: true}
}
