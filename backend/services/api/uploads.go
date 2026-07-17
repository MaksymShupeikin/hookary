package main

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const (
	singleUploadLimit int64 = 100 << 20
	defaultPartSize   int64 = 32 << 20
	maxPartSize       int64 = 5 << 30
	maxMultipartParts int64 = 10_000
	maxObjectSize     int64 = 5*(1<<40) - 5*(1<<30)
	maxPartURLBatch         = 100
)

const (
	uploadSingle    = "single_put"
	uploadMultipart = "multipart"
)

var videoContentTypes = map[string]string{
	"video/mp4":        ".mp4",
	"video/quicktime":  ".mov",
	"video/webm":       ".webm",
	"video/x-matroska": ".mkv",
	"video/mpeg":       ".mpeg",
}

type createUploadRequest struct {
	ProjectID   string `json:"project_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type uploadResponse struct {
	UploadID      string                 `json:"upload_id"`
	AssetID       string                 `json:"asset_id"`
	ProjectID     string                 `json:"project_id"`
	Mode          string                 `json:"mode"`
	URL           *string                `json:"url"`
	PartSizeBytes *int32                 `json:"part_size_bytes"`
	SizeBytes     int64                  `json:"size_bytes"`
	ContentType   string                 `json:"content_type"`
	Status        string                 `json:"status"`
	UploadedParts []uploadedPartResponse `json:"uploaded_parts"`
	ExpiresAt     string                 `json:"expires_at"`
}

type uploadedPartResponse struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
	SizeBytes  int64  `json:"size_bytes"`
}

type partURLsRequest struct {
	PartNumbers []int32 `json:"part_numbers"`
}

type partURLResponse struct {
	PartNumber int32  `json:"part_number"`
	URL        string `json:"url"`
	ExpiresAt  string `json:"expires_at"`
}

type partURLsResponse struct {
	Parts []partURLResponse `json:"parts"`
}

type completeUploadRequest struct {
	Parts []completedPartRequest `json:"parts"`
}

type completedPartRequest struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

type jobResponse struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	Stage     string     `json:"stage"`
	Status    string     `json:"status"`
	Progress  *float32   `json:"progress"`
	Attempt   int32      `json:"attempt"`
	Error     *errorBody `json:"error,omitempty"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

type presignedURLResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func (s *server) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	var req createUploadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	contentType, extension, ok := validateUploadRequest(w, r, req)
	if !ok {
		return
	}
	userID := claimsFrom(r.Context()).Subject

	if _, err := s.q.GetProject(r.Context(), db.GetProjectParams{ID: req.ProjectID, UserID: userID}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return
	} else if err != nil {
		s.fail(w, r, err, "get project for upload")
		return
	}

	assetID := shared.NewID()
	uploadID := shared.NewID()
	objectKey := fmt.Sprintf("source/%s/%s/%s/original%s", userID, req.ProjectID, assetID, extension)
	mode := uploadSingle
	var partSize *int32
	var multipartID *string
	var singleURL *string
	expiresAt := time.Now().Add(s.storage.PresignTTL())

	if req.SizeBytes > singleUploadLimit {
		mode = uploadMultipart
		size, valid := multipartPartSize(req.SizeBytes)
		if !valid {
			writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large",
				"That file is too large for a multipart upload.")
			return
		}
		value := int32(size)
		partSize = &value

		id, err := s.createMultipartUpload(r.Context(), objectKey, contentType)
		if err != nil {
			s.fail(w, r, err, "start multipart upload")
			return
		}
		multipartID = &id
	} else {
		url, expiry, err := s.presignSingleUpload(r.Context(), objectKey, contentType)
		if err != nil {
			s.fail(w, r, err, "presign single upload")
			return
		}
		singleURL = &url
		expiresAt = expiry
	}

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.cleanupStartedMultipart(r, objectKey, multipartID)
		s.fail(w, r, err, "begin upload transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)

	if _, err := qtx.CreateAsset(r.Context(), db.CreateAssetParams{
		ID: assetID, ProjectID: req.ProjectID, ObjectKey: objectKey,
		ContentType: &contentType, SizeBytes: &req.SizeBytes, Filename: req.Filename,
	}); err != nil {
		s.cleanupStartedMultipart(r, objectKey, multipartID)
		s.fail(w, r, err, "create upload asset")
		return
	}
	if _, err := qtx.CreateUpload(r.Context(), db.CreateUploadParams{
		ID: uploadID, AssetID: assetID, Mode: mode, ObjectKey: objectKey,
		S3UploadID: multipartID, PartSizeBytes: partSize,
		SizeBytes: req.SizeBytes, ContentType: contentType,
	}); err != nil {
		s.cleanupStartedMultipart(r, objectKey, multipartID)
		s.fail(w, r, err, "create upload session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.cleanupStartedMultipart(r, objectKey, multipartID)
		s.fail(w, r, err, "commit upload transaction")
		return
	}

	writeJSON(w, http.StatusCreated, uploadResponse{
		UploadID: uploadID, AssetID: assetID, ProjectID: req.ProjectID,
		Mode: mode, URL: singleURL, PartSizeBytes: partSize,
		SizeBytes: req.SizeBytes, ContentType: contentType, Status: "pending",
		UploadedParts: []uploadedPartResponse{}, ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	upload, ok := s.ownedUpload(w, r)
	if !ok {
		return
	}

	parts, err := s.uploadedParts(r, upload)
	if err != nil {
		s.fail(w, r, err, "list multipart parts")
		return
	}
	var url *string
	expires := time.Now().Add(s.storage.PresignTTL())
	if upload.Mode == uploadSingle && upload.Status == "pending" {
		value, expiry, err := s.presignSingleUpload(r.Context(), upload.ObjectKey, upload.ContentType)
		if err != nil {
			s.fail(w, r, err, "renew single upload URL")
			return
		}
		url = &value
		expires = expiry
	}

	writeJSON(w, http.StatusOK, uploadToResponse(upload, url, parts, expires))
}

func (s *server) handleUploadPartURLs(w http.ResponseWriter, r *http.Request) {
	upload, ok := s.ownedUpload(w, r)
	if !ok {
		return
	}
	if upload.Mode != uploadMultipart || upload.S3UploadID == nil || upload.Status != "pending" {
		writeError(w, r, http.StatusConflict, "upload_not_pending",
			"That multipart upload is not pending.")
		return
	}

	var req partURLsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	partNumbers, ok := validPartNumbers(w, r, req.PartNumbers, upload)
	if !ok {
		return
	}

	parts := make([]partURLResponse, 0, len(partNumbers))
	for _, number := range partNumbers {
		url, expiry, err := s.presignUploadPart(r.Context(), upload.ObjectKey, *upload.S3UploadID, number)
		if err != nil {
			s.fail(w, r, err, "presign multipart part")
			return
		}
		parts = append(parts, partURLResponse{
			PartNumber: number, URL: url, ExpiresAt: expiry.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, partURLsResponse{Parts: parts})
}

func (s *server) handleCompleteUpload(w http.ResponseWriter, r *http.Request) {
	upload, ok := s.ownedUpload(w, r)
	if !ok {
		return
	}
	if upload.Status == "aborted" {
		writeError(w, r, http.StatusConflict, "upload_aborted", "That upload was aborted.")
		return
	}

	var req completeUploadRequest
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}

	metadata, err := s.headObject(r.Context(), upload.ObjectKey)
	if err != nil && !isS3Missing(err) {
		s.fail(w, r, err, "head completed upload")
		return
	}

	parts := []uploadedPartResponse{}
	if err != nil {
		if upload.Mode != uploadMultipart || upload.S3UploadID == nil {
			writeError(w, r, http.StatusConflict, "upload_missing",
				"The uploaded object is not present in storage.")
			return
		}

		parts, err = s.partsForCompletion(r, upload, req.Parts)
		if err != nil {
			writeError(w, r, http.StatusConflict, "parts_incomplete", err.Error())
			return
		}
		if err := s.finishMultipartUpload(r.Context(), upload.ObjectKey, *upload.S3UploadID, parts); err != nil {
			s.fail(w, r, err, "complete multipart upload")
			return
		}
		metadata, err = s.headObject(r.Context(), upload.ObjectKey)
		if err != nil {
			s.fail(w, r, err, "verify completed multipart upload")
			return
		}
	} else if upload.Mode == uploadMultipart {
		// The object can already exist while Postgres is still pending if R2
		// completed the request and the API crashed before committing. Rebuild
		// the bounded part metadata from the declared file size.
		parts = expectedCompletedParts(upload, req.Parts)
	}

	if metadata.SizeBytes != upload.SizeBytes {
		if deleteErr := s.deleteObject(r.Context(), upload.ObjectKey); deleteErr != nil {
			s.log.Error().Err(deleteErr).Msg("delete size-mismatched upload")
		}
		writeError(w, r, http.StatusConflict, "size_mismatch",
			"The uploaded object size does not match the declared file size.")
		return
	}

	job, err := s.recordCompletedUpload(r, upload, parts)
	if err != nil {
		s.fail(w, r, err, "record completed upload")
		return
	}
	writeJSON(w, http.StatusAccepted, jobToResponse(job))
}

func (s *server) handleAbortUpload(w http.ResponseWriter, r *http.Request) {
	upload, ok := s.ownedUpload(w, r)
	if !ok {
		return
	}
	if upload.Status != "pending" {
		writeError(w, r, http.StatusConflict, "upload_not_pending", "Only a pending upload can be aborted.")
		return
	}
	if upload.Mode == uploadMultipart && upload.S3UploadID != nil {
		if err := s.abortMultipartUpload(r.Context(), upload.ObjectKey, *upload.S3UploadID); err != nil {
			s.fail(w, r, err, "abort multipart upload")
			return
		}
	}
	if err := s.deleteObject(r.Context(), upload.ObjectKey); err != nil {
		s.fail(w, r, err, "delete aborted upload object")
		return
	}
	deleted, err := s.q.DeletePendingUploadAsset(r.Context(), db.DeletePendingUploadAssetParams{
		ID: upload.UploadID, UserID: claimsFrom(r.Context()).Subject,
	})
	if err != nil {
		s.fail(w, r, err, "delete aborted upload")
		return
	}
	if deleted == 0 {
		writeError(w, r, http.StatusConflict, "upload_not_pending", "Only a pending upload can be aborted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
