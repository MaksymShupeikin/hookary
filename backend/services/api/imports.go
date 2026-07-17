package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const publicURLValidationTimeout = 8 * time.Second

type createURLImportRequest struct {
	ProjectID string `json:"project_id"`
	URL       string `json:"url"`
}

func (s *server) handleCreateURLImport(w http.ResponseWriter, r *http.Request) {
	var req createURLImportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !shared.ValidID(req.ProjectID) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return
	}
	userID := claimsFrom(r.Context()).Subject
	if _, err := s.q.GetProject(r.Context(), db.GetProjectParams{
		ID: req.ProjectID, UserID: userID,
	}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return
	} else if err != nil {
		s.fail(w, r, err, "get project for URL import")
		return
	}

	validationContext, cancel := context.WithTimeout(r.Context(), publicURLValidationTimeout)
	defer cancel()
	parsed, err := shared.ValidatePublicURL(validationContext, req.URL)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "url_rejected",
			"Use a public http or https video URL.")
		return
	}

	assetID := shared.NewID()
	objectKey := fmt.Sprintf("source/%s/%s/%s/original.mp4", userID, req.ProjectID, assetID)
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.fail(w, r, err, "begin URL import transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := s.q.WithTx(tx)
	sourceURL := strings.TrimSpace(parsed.String())
	if _, err := q.CreatePublicURLAsset(r.Context(), db.CreatePublicURLAssetParams{
		ID: assetID, ProjectID: req.ProjectID, SourceUrl: &sourceURL, ObjectKey: objectKey,
	}); err != nil {
		s.fail(w, r, err, "create URL asset")
		return
	}
	if err := q.SetProjectProcessing(r.Context(), db.SetProjectProcessingParams{
		ID: req.ProjectID, UserID: userID,
	}); err != nil {
		s.fail(w, r, err, "mark URL project processing")
		return
	}
	idempotencyKey := "ingest:" + assetID
	job, err := q.CreateIngestJob(r.Context(), db.CreateIngestJobParams{
		ID: shared.NewID(), ProjectID: req.ProjectID, AssetID: &assetID,
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		s.fail(w, r, err, "create URL ingest job")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.fail(w, r, err, "commit URL import")
		return
	}

	writeJSON(w, http.StatusAccepted, jobToResponse(job))
}
