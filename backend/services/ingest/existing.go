package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func (p *ingestProcessor) processExisting(
	ctx context.Context,
	jobID string,
	asset db.Asset,
	previous storedAssetMetadata,
	workDir string,
	report shared.ProgressReporter,
) (ingestResult, error) {
	localPath := filepath.Join(workDir, "existing-source")
	size, checksum, err := downloadObject(
		ctx, p.storage, asset.ObjectKey, localPath, asset.SizeBytes, p.cfg.MaxMediaBytes,
	)
	if errors.Is(err, errMediaTooLarge) {
		return ingestResult{}, reject("media_too_large", "The source video is larger than the ingest limit.")
	}
	if err != nil {
		return ingestResult{}, retry("source_unavailable", "Could not reload the existing source.")
	}
	if err := report(0.3); err != nil {
		return ingestResult{}, err
	}
	probe, err := probeMedia(ctx, p.cfg, localPath)
	if err != nil {
		return ingestResult{}, reject("media_invalid", err.Error())
	}
	result := ingestResult{
		ObjectKey: asset.ObjectKey, ContentType: probe.ContentType,
		SizeBytes: size, Checksum: checksum, Probe: probe,
		URL: urlMediaMetadata{
			ID: previous.ExternalID, Title: previous.Title, Extractor: previous.Extractor,
		},
	}
	if asset.SourceKind == "public_url" && asset.SourceUrl != nil {
		if _, validationErr := shared.ValidatePublicURL(ctx, *asset.SourceUrl); validationErr == nil {
			metadata, inspectErr := inspectURL(ctx, p.cfg, p.proxyAddress, *asset.SourceUrl)
			if inspectErr == nil {
				result.URL = metadata
				result.Transcript = p.prepareCaptions(
					ctx, jobID, *asset.SourceUrl, workDir, metadata, probe,
				)
			}
		}
	}
	if err := report(0.55); err != nil {
		return ingestResult{}, err
	}
	if result.Transcript == nil {
		result.AudioChunks, err = p.prepareAudio(ctx, jobID, localPath, workDir, probe)
		if err != nil {
			return ingestResult{}, err
		}
	}
	if err := report(0.94); err != nil {
		return ingestResult{}, err
	}
	return result, nil
}
