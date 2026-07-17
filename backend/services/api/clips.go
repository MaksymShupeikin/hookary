package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type clipResponse struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	AssetID     string  `json:"asset_id"`
	Title       string  `json:"title"`
	Hook        *string `json:"hook"`
	Reason      *string `json:"reason"`
	Score       float32 `json:"score"`
	ContextRisk *string `json:"context_risk"`
	StartMS     int64   `json:"start_ms"`
	EndMS       int64   `json:"end_ms"`
	Status      string  `json:"status"`
	CanRender   bool    `json:"can_render"`
}

type clipListResponse struct {
	Clips []clipResponse `json:"clips"`
}

type renderClipsRequest struct {
	ClipIDs []string `json:"clip_ids"`
}

type renderClipsResponse struct {
	Jobs []jobResponse `json:"jobs"`
}

type clipDownloadResponse struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	ExpiresAt string `json:"expires_at"`
}

type clipPreviewResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func (s *server) handleListProjectClips(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "project_id")
	if !ok {
		return
	}
	userID := claimsFrom(r.Context()).Subject
	clips, err := s.q.ListOwnedProjectClips(r.Context(), db.ListOwnedProjectClipsParams{
		ProjectID: projectID, UserID: userID,
	})
	if err != nil {
		s.fail(w, r, err, "list project clips")
		return
	}
	if len(clips) == 0 {
		if _, err := s.q.GetProject(r.Context(), db.GetProjectParams{
			ID: projectID, UserID: userID,
		}); errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
			return
		} else if err != nil {
			s.fail(w, r, err, "get project for clips")
			return
		}
	}
	items := make([]clipResponse, 0, len(clips))
	for _, clip := range clips {
		items = append(items, clipToResponse(clip))
	}
	writeJSON(w, http.StatusOK, clipListResponse{Clips: items})
}

func clipToResponse(clip db.Clip) clipResponse {
	return clipResponse{
		ID: clip.ID, ProjectID: clip.ProjectID, AssetID: clip.AssetID,
		Title: clip.Title, Hook: clip.Hook, Reason: clip.Reason, Score: clip.Score,
		ContextRisk: clip.ContextRisk, StartMS: clip.StartMs, EndMS: clip.EndMs,
		Status: clip.Status, CanRender: clipRenderAvailable(clip),
	}
}

func clipRenderAvailable(clip db.Clip) bool {
	switch clip.Status {
	case "candidate", "failed":
		return true
	case "rendered":
		return clip.OutputKey != nil && !strings.HasSuffix(*clip.OutputKey, "/v4.mp4")
	default:
		return false
	}
}

func (s *server) handleRenderProjectClips(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "project_id")
	if !ok {
		return
	}
	var req renderClipsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.ClipIDs) == 0 || len(req.ClipIDs) > 5 {
		writeError(w, r, http.StatusBadRequest, "invalid_clip_selection",
			"Select between one and five suggested clips.")
		return
	}
	seen := make(map[string]struct{}, len(req.ClipIDs))
	for _, clipID := range req.ClipIDs {
		if !shared.ValidID(clipID) {
			writeError(w, r, http.StatusNotFound, "not_found", "No such clip.")
			return
		}
		if _, duplicate := seen[clipID]; duplicate {
			writeError(w, r, http.StatusBadRequest, "duplicate_clip",
				"A clip can only be selected once.")
			return
		}
		seen[clipID] = struct{}{}
	}

	userID := claimsFrom(r.Context()).Subject
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.fail(w, r, err, "begin clip render transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := s.q.WithTx(tx)
	jobs := make([]db.Job, 0, len(req.ClipIDs))
	for _, clipID := range req.ClipIDs {
		clip, err := q.GetOwnedProjectClip(r.Context(), db.GetOwnedProjectClipParams{
			ID: clipID, ProjectID: projectID, UserID: userID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "not_found", "No such clip.")
			return
		}
		if err != nil {
			s.fail(w, r, err, "get clip for render")
			return
		}
		if clip.Status == "rendered" && !clipRenderAvailable(clip) {
			writeError(w, r, http.StatusConflict, "clip_already_rendered",
				"One of the selected clips already uses the latest render style.")
			return
		}
		if clip.Status != "candidate" && clip.Status != "failed" &&
			clip.Status != "rendering" && clip.Status != "rendered" {
			writeError(w, r, http.StatusConflict, "clip_not_renderable",
				"One of the selected clips cannot be rendered right now.")
			return
		}
		key := "render:v4:" + clip.ID
		job, err := q.QueueClipRenderJob(r.Context(), db.QueueClipRenderJobParams{
			ID: shared.NewID(), ProjectID: clip.ProjectID, AssetID: &clip.AssetID,
			ClipID: &clip.ID, IdempotencyKey: &key,
		})
		if err != nil {
			s.fail(w, r, err, "queue clip render")
			return
		}
		if _, err := q.MarkClipRendering(r.Context(), clip.ID); err != nil {
			s.fail(w, r, err, "mark clip rendering")
			return
		}
		jobs = append(jobs, job)
	}
	if err := q.SetProjectProcessing(r.Context(), db.SetProjectProcessingParams{
		ID: projectID, UserID: userID,
	}); err != nil {
		s.fail(w, r, err, "mark render project processing")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.fail(w, r, err, "commit clip render transaction")
		return
	}
	responses := make([]jobResponse, 0, len(jobs))
	for _, job := range jobs {
		shared.PublishJobEvent(s.nats, job, s.log)
		responses = append(responses, jobToResponse(job))
	}
	writeJSON(w, http.StatusAccepted, renderClipsResponse{Jobs: responses})
}

func (s *server) handleClipDownloadURL(w http.ResponseWriter, r *http.Request) {
	clipID, ok := pathID(w, r, "clip_id")
	if !ok {
		return
	}
	clip, err := s.q.GetOwnedClip(r.Context(), db.GetOwnedClipParams{
		ID: clipID, UserID: claimsFrom(r.Context()).Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such clip.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get clip download")
		return
	}
	if clip.Status != "rendered" || clip.OutputKey == nil {
		writeError(w, r, http.StatusConflict, "clip_not_rendered",
			"This clip is not ready to download yet.")
		return
	}
	if _, err := s.headObject(r.Context(), *clip.OutputKey); err != nil {
		writeError(w, r, http.StatusConflict, "clip_output_missing",
			"The rendered video is not available in storage.")
		return
	}
	filename := "hookary-" + clip.ID + ".mp4"
	url, expiry, err := s.presignDownload(r.Context(), *clip.OutputKey, filename)
	if err != nil {
		s.fail(w, r, err, "presign clip download")
		return
	}
	writeJSON(w, http.StatusOK, clipDownloadResponse{
		URL: url, Filename: filename, ExpiresAt: expiry.UTC().Format(time.RFC3339),
	})
}

func (s *server) handleClipPreviewURL(w http.ResponseWriter, r *http.Request) {
	clipID, ok := pathID(w, r, "clip_id")
	if !ok {
		return
	}
	clip, err := s.q.GetOwnedClip(r.Context(), db.GetOwnedClipParams{
		ID: clipID, UserID: claimsFrom(r.Context()).Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such clip.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get rendered clip preview")
		return
	}
	if clip.Status != "rendered" || clip.OutputKey == nil {
		writeError(w, r, http.StatusConflict, "clip_not_rendered",
			"This clip is not ready to preview yet.")
		return
	}
	if _, err := s.headObject(r.Context(), *clip.OutputKey); err != nil {
		writeError(w, r, http.StatusConflict, "clip_output_missing",
			"The rendered video is not available in storage.")
		return
	}
	url, expiry, err := s.presignPreview(r.Context(), *clip.OutputKey)
	if err != nil {
		s.fail(w, r, err, "presign rendered clip preview")
		return
	}
	writeJSON(w, http.StatusOK, clipPreviewResponse{
		URL: url, ExpiresAt: expiry.UTC().Format(time.RFC3339),
	})
}
