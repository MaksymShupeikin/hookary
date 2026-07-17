package main

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func (s *server) ownedUpload(w http.ResponseWriter, r *http.Request) (db.GetUploadRow, bool) {
	uploadID, ok := pathID(w, r, "upload_id")
	if !ok {
		return db.GetUploadRow{}, false
	}
	upload, err := s.q.GetUpload(r.Context(), db.GetUploadParams{
		ID: uploadID, UserID: claimsFrom(r.Context()).Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such upload.")
		return db.GetUploadRow{}, false
	}
	if err != nil {
		s.fail(w, r, err, "get upload")
		return db.GetUploadRow{}, false
	}
	return upload, true
}

func (s *server) uploadedParts(r *http.Request, upload db.GetUploadRow) ([]uploadedPartResponse, error) {
	if upload.Mode != uploadMultipart {
		return []uploadedPartResponse{}, nil
	}
	if upload.Status == "completed" {
		var parts []uploadedPartResponse
		if err := json.Unmarshal(upload.Parts, &parts); err != nil {
			return nil, err
		}
		return parts, nil
	}
	if upload.S3UploadID == nil {
		return nil, errors.New("multipart upload has no storage upload id")
	}
	parts, err := s.listMultipartParts(r.Context(), upload.ObjectKey, *upload.S3UploadID)
	if isS3Missing(err) {
		// A completed object plus a pending row is the recoverable crash window
		// handled by POST /complete. There are no upload parts left to list.
		if _, headErr := s.headObject(r.Context(), upload.ObjectKey); headErr == nil {
			return []uploadedPartResponse{}, nil
		}
	}
	return parts, err
}

func uploadToResponse(
	upload db.GetUploadRow,
	url *string,
	parts []uploadedPartResponse,
	expires time.Time,
) uploadResponse {
	return uploadResponse{
		UploadID: upload.UploadID, AssetID: upload.AssetID, ProjectID: upload.ProjectID,
		Mode: upload.Mode, URL: url, PartSizeBytes: upload.PartSizeBytes,
		SizeBytes: upload.SizeBytes, ContentType: upload.ContentType,
		Status: upload.Status, UploadedParts: parts,
		ExpiresAt: expires.UTC().Format(time.RFC3339),
	}
}

func (s *server) recordCompletedUpload(
	r *http.Request,
	upload db.GetUploadRow,
	parts []uploadedPartResponse,
) (db.Job, error) {
	encoded, err := json.Marshal(parts)
	if err != nil {
		return db.Job{}, err
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return db.Job{}, err
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)

	if _, err := qtx.CompleteUpload(r.Context(), db.CompleteUploadParams{
		ID: upload.UploadID, Parts: encoded,
	}); err != nil {
		return db.Job{}, err
	}
	userID := claimsFrom(r.Context()).Subject
	if err := qtx.SetProjectProcessing(r.Context(), db.SetProjectProcessingParams{
		ID: upload.ProjectID, UserID: userID,
	}); err != nil {
		return db.Job{}, err
	}
	assetID := upload.AssetID
	idempotencyKey := "ingest:" + assetID
	job, err := qtx.CreateIngestJob(r.Context(), db.CreateIngestJobParams{
		ID: shared.NewID(), ProjectID: upload.ProjectID, AssetID: &assetID,
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		return db.Job{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return db.Job{}, err
	}
	return job, nil
}

func jobToResponse(job db.Job) jobResponse {
	var jobError *errorBody
	if job.ErrorCode != nil {
		jobError = &errorBody{Code: *job.ErrorCode, Message: stringValue(job.ErrorMessage)}
	}
	return jobResponse{
		ID: job.ID, ProjectID: job.ProjectID, Stage: job.Stage, Status: job.Status,
		Progress: job.Progress, Attempt: job.Attempt, Error: jobError,
		CreatedAt: job.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: job.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func validateUploadRequest(
	w http.ResponseWriter,
	r *http.Request,
	req createUploadRequest,
) (contentType, extension string, ok bool) {
	if !shared.ValidID(req.ProjectID) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
		return "", "", false
	}
	if strings.TrimSpace(req.Filename) == "" || len(req.Filename) > 512 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "filename is required.")
		return "", "", false
	}
	if req.SizeBytes <= 0 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "size_bytes must be positive.")
		return "", "", false
	}
	if req.SizeBytes > maxObjectSize {
		writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large",
			"That file is larger than object storage supports.")
		return "", "", false
	}

	contentType = strings.ToLower(strings.TrimSpace(strings.Split(req.ContentType, ";")[0]))
	extension, ok = videoContentTypes[contentType]
	if !ok {
		candidate := strings.ToLower(filepath.Ext(req.Filename))
		for mediaType, ext := range videoContentTypes {
			if candidate == ext && (contentType == "" || contentType == "application/octet-stream") {
				return mediaType, ext, true
			}
		}
		writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Choose an MP4, MOV, WebM, MKV, or MPEG video.")
		return "", "", false
	}
	return contentType, extension, true
}

func multipartPartSize(sizeBytes int64) (int64, bool) {
	partSize := defaultPartSize
	if countParts(sizeBytes, partSize) > maxMultipartParts {
		partSize = int64(math.Ceil(float64(sizeBytes)/float64(maxMultipartParts)/(1<<20))) << 20
	}
	return partSize, partSize >= 5<<20 && partSize <= maxPartSize
}

func countParts(sizeBytes, partSize int64) int64 {
	return (sizeBytes + partSize - 1) / partSize
}

func validPartNumbers(
	w http.ResponseWriter,
	r *http.Request,
	numbers []int32,
	upload db.GetUploadRow,
) ([]int32, bool) {
	if len(numbers) == 0 || len(numbers) > maxPartURLBatch || upload.PartSizeBytes == nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request",
			"Request between 1 and 100 multipart part numbers.")
		return nil, false
	}
	maxPart := countParts(upload.SizeBytes, int64(*upload.PartSizeBytes))
	seen := make(map[int32]struct{}, len(numbers))
	for _, number := range numbers {
		if number < 1 || int64(number) > maxPart {
			writeError(w, r, http.StatusBadRequest, "invalid_part_number",
				"A requested part number is outside this upload.")
			return nil, false
		}
		if _, exists := seen[number]; exists {
			writeError(w, r, http.StatusBadRequest, "invalid_part_number",
				"Part numbers must not be repeated.")
			return nil, false
		}
		seen[number] = struct{}{}
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	return numbers, true
}

func (s *server) partsForCompletion(
	r *http.Request,
	upload db.GetUploadRow,
	requested []completedPartRequest,
) ([]uploadedPartResponse, error) {
	if upload.S3UploadID == nil || upload.PartSizeBytes == nil {
		return nil, errors.New("The multipart session is incomplete.")
	}
	stored, err := s.listMultipartParts(r.Context(), upload.ObjectKey, *upload.S3UploadID)
	if err != nil {
		return nil, errors.New("The multipart session is no longer available.")
	}
	expectedCount := countParts(upload.SizeBytes, int64(*upload.PartSizeBytes))
	if int64(len(stored)) != expectedCount || len(requested) != len(stored) {
		return nil, errors.New("Not every multipart part has been uploaded.")
	}

	requestETags := make(map[int32]string, len(requested))
	for _, part := range requested {
		if part.PartNumber < 1 || part.ETag == "" {
			return nil, errors.New("A multipart part is missing its ETag.")
		}
		if _, duplicate := requestETags[part.PartNumber]; duplicate {
			return nil, errors.New("A multipart part was repeated.")
		}
		requestETags[part.PartNumber] = normalizedETag(part.ETag)
	}

	for i, part := range stored {
		number := int32(i + 1)
		if part.PartNumber != number || normalizedETag(part.ETag) != requestETags[number] {
			return nil, errors.New("A multipart ETag does not match storage.")
		}
		expectedSize := int64(*upload.PartSizeBytes)
		if number == int32(expectedCount) {
			expectedSize = upload.SizeBytes - int64(number-1)*int64(*upload.PartSizeBytes)
		}
		if part.SizeBytes != expectedSize {
			return nil, errors.New("A multipart part has the wrong size.")
		}
	}
	return stored, nil
}

func expectedCompletedParts(
	upload db.GetUploadRow,
	requested []completedPartRequest,
) []uploadedPartResponse {
	if upload.PartSizeBytes == nil {
		return []uploadedPartResponse{}
	}
	parts := make([]uploadedPartResponse, 0, len(requested))
	for _, part := range requested {
		size := int64(*upload.PartSizeBytes)
		if offset := int64(part.PartNumber-1) * size; offset+size > upload.SizeBytes {
			size = upload.SizeBytes - offset
		}
		parts = append(parts, uploadedPartResponse{
			PartNumber: part.PartNumber, ETag: part.ETag, SizeBytes: size,
		})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts
}

func normalizedETag(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), `"`))
}

func (s *server) cleanupStartedMultipart(r *http.Request, key string, uploadID *string) {
	if uploadID == nil {
		return
	}
	if err := s.abortMultipartUpload(r.Context(), key, *uploadID); err != nil {
		s.log.Error().Err(err).Msg("clean up unrecorded multipart upload")
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
