package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type assetResponse struct {
	ID               string  `json:"id"`
	ProjectID        string  `json:"project_id"`
	SourceKind       string  `json:"source_kind"`
	Title            string  `json:"title"`
	Status           string  `json:"status"`
	PreviewReady     bool    `json:"preview_ready"`
	TranscriptReady  bool    `json:"transcript_ready"`
	TranscriptStatus string  `json:"transcript_status"`
	AnalysisStatus   string  `json:"analysis_status"`
	ContentType      *string `json:"content_type"`
	SizeBytes        *int64  `json:"size_bytes"`
	DurationMS       *int64  `json:"duration_ms"`
	Width            *int32  `json:"width"`
	Height           *int32  `json:"height"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type assetListResponse struct {
	Assets []assetResponse `json:"assets"`
}

type assetPreviewResponse struct {
	URL          string  `json:"url"`
	SubtitlesURL *string `json:"subtitles_url,omitempty"`
	ExpiresAt    string  `json:"expires_at"`
}

type assetMetadata struct {
	Title string `json:"title"`
}

func (s *server) handleListProjectAssets(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "project_id")
	if !ok {
		return
	}
	userID := claimsFrom(r.Context()).Subject
	if _, err := s.q.GetProject(r.Context(), db.GetProjectParams{
		ID: projectID, UserID: userID,
	}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return
	} else if err != nil {
		s.fail(w, r, err, "get project for asset list")
		return
	}

	assets, err := s.q.ListProjectAssets(r.Context(), db.ListProjectAssetsParams{
		ProjectID: projectID, UserID: userID,
	})
	if err != nil {
		s.fail(w, r, err, "list project assets")
		return
	}
	items := make([]assetResponse, 0, len(assets))
	for _, asset := range assets {
		items = append(items, assetToResponse(asset))
	}
	writeJSON(w, http.StatusOK, assetListResponse{Assets: items})
}

func assetToResponse(asset db.ListProjectAssetsRow) assetResponse {
	title := "Uploaded video"
	if asset.SourceKind == "public_url" {
		title = "YouTube video"
	}
	var metadata assetMetadata
	if json.Unmarshal(asset.Metadata, &metadata) == nil && metadata.Title != "" {
		title = metadata.Title
	}
	return assetResponse{
		ID: asset.ID, ProjectID: asset.ProjectID, SourceKind: asset.SourceKind,
		Title: title, Status: asset.Status,
		PreviewReady:     asset.PreviewReady != nil && *asset.PreviewReady,
		TranscriptReady:  asset.TranscriptReady,
		TranscriptStatus: asset.TranscriptStatus,
		AnalysisStatus:   asset.AnalysisStatus,
		ContentType:      asset.ContentType, SizeBytes: asset.SizeBytes,
		DurationMS: asset.DurationMs, Width: asset.Width, Height: asset.Height,
		CreatedAt: asset.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: asset.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func (s *server) handleAssetPreviewURL(w http.ResponseWriter, r *http.Request) {
	assetID, ok := pathID(w, r, "asset_id")
	if !ok {
		return
	}
	asset, err := s.q.GetOwnedAsset(r.Context(), db.GetOwnedAssetParams{
		ID: assetID, UserID: claimsFrom(r.Context()).Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such asset.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get asset preview")
		return
	}
	if asset.Status != "ready" && !asset.UploadCompleted {
		writeError(w, r, http.StatusConflict, "source_not_ready",
			"The source video is not ready for preview yet.")
		return
	}
	metadata, err := s.headObject(r.Context(), asset.ObjectKey)
	if err != nil || asset.SizeBytes == nil || metadata.SizeBytes != *asset.SizeBytes {
		writeError(w, r, http.StatusConflict, "source_missing",
			"The source object is not available in storage.")
		return
	}
	url, expiry, err := s.presignPreview(r.Context(), asset.ObjectKey)
	if err != nil {
		s.fail(w, r, err, "presign asset preview")
		return
	}
	var subtitlesURL *string
	transcript, transcriptErr := s.q.GetOwnedAssetTranscript(
		r.Context(),
		db.GetOwnedAssetTranscriptParams{
			AssetID: asset.ID, UserID: claimsFrom(r.Context()).Subject,
		},
	)
	if transcriptErr == nil {
		value, _, presignErr := s.presignPreview(r.Context(), transcript.ObjectKey)
		if presignErr != nil {
			s.fail(w, r, presignErr, "presign asset subtitles")
			return
		}
		subtitlesURL = &value
	} else if !errors.Is(transcriptErr, pgx.ErrNoRows) {
		s.fail(w, r, transcriptErr, "get asset transcript")
		return
	}
	writeJSON(w, http.StatusOK, assetPreviewResponse{
		URL: url, SubtitlesURL: subtitlesURL,
		ExpiresAt: expiry.UTC().Format(time.RFC3339),
	})
}

func (s *server) handleAnalyzeAsset(w http.ResponseWriter, r *http.Request) {
	assetID, ok := pathID(w, r, "asset_id")
	if !ok {
		return
	}
	userID := claimsFrom(r.Context()).Subject
	asset, err := s.q.GetOwnedAsset(r.Context(), db.GetOwnedAssetParams{
		ID: assetID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such asset.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get asset for analysis")
		return
	}
	if asset.Status != "ready" {
		writeError(w, r, http.StatusConflict, "source_not_ready",
			"The source must finish ingest before analysis starts.")
		return
	}
	if _, err := s.q.GetTranscriptByAsset(r.Context(), asset.ID); err == nil {
		writeError(w, r, http.StatusConflict, "transcript_exists",
			"This source already has a transcript.")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.fail(w, r, err, "check transcript before analysis")
		return
	}

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.fail(w, r, err, "begin analysis transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := s.q.WithTx(tx)
	idempotencyKey := "ingest:v2:" + asset.ID
	job, err := q.QueueAssetReingest(r.Context(), db.QueueAssetReingestParams{
		ID: shared.NewID(), ProjectID: asset.ProjectID, AssetID: &asset.ID,
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		s.fail(w, r, err, "queue asset analysis")
		return
	}
	if err := q.SetProjectProcessing(r.Context(), db.SetProjectProcessingParams{
		ID: asset.ProjectID, UserID: userID,
	}); err != nil {
		s.fail(w, r, err, "mark analysis project processing")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.fail(w, r, err, "commit analysis transaction")
		return
	}
	shared.PublishJobEvent(s.nats, job, s.log)
	writeJSON(w, http.StatusAccepted, jobToResponse(job))
}
