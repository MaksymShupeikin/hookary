package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const (
	defaultProjectLimit = 20
	maxProjectLimit     = 100
)

type projectResponse struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	AssetCount int64  `json:"asset_count"`
	ClipCount  int64  `json:"clip_count"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type projectListResponse struct {
	Projects   []projectResponse `json:"projects"`
	NextCursor *string           `json:"next_cursor"`
}

type createProjectRequest struct {
	Title string `json:"title"`
}

func projectRowToResponse(p db.GetProjectRow) projectResponse {
	return projectResponse{
		ID:         p.ID,
		Title:      p.Title,
		Status:     p.Status,
		AssetCount: p.AssetCount,
		ClipCount:  p.ClipCount,
		CreatedAt:  p.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:  p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func listProjectRowToResponse(p db.ListProjectsRow) projectResponse {
	return projectResponse{
		ID:         p.ID,
		Title:      p.Title,
		Status:     p.Status,
		AssetCount: p.AssetCount,
		ClipCount:  p.ClipCount,
		CreatedAt:  p.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:  p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func createdProjectToResponse(p db.Project) projectResponse {
	return projectResponse{
		ID:        p.ID,
		Title:     p.Title,
		Status:    p.Status,
		CreatedAt: p.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func (s *server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" || len([]rune(title)) > 200 {
		writeError(w, r, http.StatusBadRequest, "invalid_request",
			"title must contain between 1 and 200 characters.")
		return
	}

	project, err := s.q.CreateProject(r.Context(), db.CreateProjectParams{
		ID:     shared.NewID(),
		UserID: claimsFrom(r.Context()).Subject,
		Title:  title,
	})
	if err != nil {
		s.fail(w, r, err, "create project")
		return
	}

	writeJSON(w, http.StatusCreated, createdProjectToResponse(project))
}

func (s *server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	limit, ok := projectLimit(w, r)
	if !ok {
		return
	}

	cursor := r.URL.Query().Get("cursor")
	if cursor != "" && !shared.ValidID(cursor) {
		writeError(w, r, http.StatusBadRequest, "invalid_cursor", "That cursor is not valid.")
		return
	}

	rows, err := s.q.ListProjects(r.Context(), db.ListProjectsParams{
		UserID:  claimsFrom(r.Context()).Subject,
		Column2: cursor,
		Limit:   int32(limit + 1),
	})
	if err != nil {
		s.fail(w, r, err, "list projects")
		return
	}

	var next *string
	if len(rows) > limit {
		value := rows[limit-1].ID
		next = &value
		rows = rows[:limit]
	}

	projects := make([]projectResponse, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, listProjectRowToResponse(row))
	}

	writeJSON(w, http.StatusOK, projectListResponse{Projects: projects, NextCursor: next})
}

func (s *server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "project_id")
	if !ok {
		return
	}

	project, err := s.q.GetProject(r.Context(), db.GetProjectParams{
		ID: projectID, UserID: claimsFrom(r.Context()).Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get project")
		return
	}

	writeJSON(w, http.StatusOK, projectRowToResponse(project))
}

func (s *server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "project_id")
	if !ok {
		return
	}
	userID := claimsFrom(r.Context()).Subject

	if _, err := s.q.GetProject(r.Context(), db.GetProjectParams{ID: projectID, UserID: userID}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return
	} else if err != nil {
		s.fail(w, r, err, "get project for delete")
		return
	}

	pending, err := s.q.ListPendingMultipartUploads(r.Context(), db.ListPendingMultipartUploadsParams{
		ID: projectID, UserID: userID,
	})
	if err != nil {
		s.fail(w, r, err, "list project multipart uploads")
		return
	}
	for _, upload := range pending {
		if upload.S3UploadID == nil {
			continue
		}
		_, err := s.storage.Client.AbortMultipartUpload(r.Context(), &s3.AbortMultipartUploadInput{
			Bucket: aws.String(s.storage.Bucket),
			Key:    aws.String(upload.ObjectKey), UploadId: upload.S3UploadID,
		})
		if err != nil && !isS3Missing(err) {
			s.fail(w, r, err, "abort project multipart upload")
			return
		}
	}

	for _, prefix := range []string{
		"source/" + userID + "/" + projectID + "/",
		"output/" + userID + "/" + projectID + "/",
	} {
		if err := s.deleteObjectPrefix(r.Context(), prefix); err != nil {
			s.fail(w, r, err, "delete project objects")
			return
		}
	}

	deleted, err := s.q.DeleteProject(r.Context(), db.DeleteProjectParams{ID: projectID, UserID: userID})
	if err != nil {
		s.fail(w, r, err, "delete project")
		return
	}
	if deleted == 0 {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteObjectPrefix(ctx context.Context, prefix string) error {
	// Delete from the first page repeatedly. Continuing with a token after
	// deleting that token's page is backend-dependent; restarting from the
	// prefix is unambiguous and R2 is strongly consistent.
	for {
		page, err := s.storage.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(s.storage.Bucket), Prefix: aws.String(prefix),
			MaxKeys: aws.Int32(1000),
		})
		if err != nil {
			return err
		}
		if len(page.Contents) == 0 {
			return nil
		}

		objects := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, object := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{Key: object.Key})
		}
		if _, err := s.storage.Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.storage.Bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		}); err != nil {
			return err
		}
	}
}

func projectLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultProjectLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxProjectLimit {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 100.")
		return 0, false
	}
	return limit, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if !shared.ValidID(id) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such resource.")
		return "", false
	}
	return id, true
}
