package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func newStorageTestServer(t *testing.T) *server {
	t.Helper()

	s := newTestServer(t)
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT is not set; run `make go-test-integration`")
	}
	cfg := s.cfg
	cfg.S3 = shared.S3Config{
		Endpoint:        endpoint,
		PublicEndpoint:  endpoint,
		Region:          "auto",
		Bucket:          os.Getenv("TEST_S3_BUCKET"),
		AccessKeyID:     os.Getenv("TEST_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("TEST_S3_SECRET_ACCESS_KEY"),
		ForcePathStyle:  true,
		PresignTTL:      15 * time.Minute,
	}
	storage, err := shared.NewStorage(context.Background(), cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("build test storage: %v", err)
	}
	if err := storage.Probe()(context.Background()); err != nil {
		t.Fatalf("probe test storage: %v", err)
	}
	s.storage = storage
	return s
}

func authenticatedJSON(
	t *testing.T,
	s *server,
	userID, method, path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token, _, err := s.mintAccessToken(userID, shared.NewID())
	if err != nil {
		t.Fatalf("mint access token: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	s.newRouter(func(context.Context) error { return nil }).ServeHTTP(rec, req)
	return rec
}

func createProjectForTest(t *testing.T, s *server, userID string) db.Project {
	t.Helper()
	project, err := s.q.CreateProject(context.Background(), db.CreateProjectParams{
		ID: shared.NewID(), UserID: userID, Title: "Test project",
	})
	if err != nil {
		t.Fatalf("create test project: %v", err)
	}
	return project
}

func decodeResponse[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(rec.Body).Decode(&value); err != nil {
		t.Fatalf("decode response with status %d: %v; body=%s", rec.Code, err, rec.Body.String())
	}
	return value
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, want, rec.Body.String())
	}
}

func TestProjectCRUDIsScopedToTheSignedInUser(t *testing.T) {
	s := newStorageTestServer(t)
	owner := createTestUser(t, s)
	other := createTestUser(t, s)

	created := authenticatedJSON(t, s, owner.ID, http.MethodPost, "/v1/projects", map[string]any{
		"title": "  First source  ",
	})
	expectStatus(t, created, http.StatusCreated)
	project := decodeResponse[projectResponse](t, created)
	if project.Title != "First source" || project.Status != "empty" {
		t.Fatalf("unexpected project: %+v", project)
	}

	listed := authenticatedJSON(t, s, owner.ID, http.MethodGet, "/v1/projects?limit=1", nil)
	expectStatus(t, listed, http.StatusOK)
	projects := decodeResponse[projectListResponse](t, listed)
	if len(projects.Projects) != 1 || projects.Projects[0].ID != project.ID {
		t.Fatalf("listed projects = %+v", projects.Projects)
	}

	denied := authenticatedJSON(t, s, other.ID, http.MethodGet, "/v1/projects/"+project.ID, nil)
	expectStatus(t, denied, http.StatusNotFound)

	deleted := authenticatedJSON(t, s, owner.ID, http.MethodDelete, "/v1/projects/"+project.ID, nil)
	expectStatus(t, deleted, http.StatusNoContent)

	missing := authenticatedJSON(t, s, owner.ID, http.MethodGet, "/v1/projects/"+project.ID, nil)
	expectStatus(t, missing, http.StatusNotFound)
}

func TestSingleUploadGoesDirectlyToStorageAndCompletesIdempotently(t *testing.T) {
	s := newStorageTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	payload := []byte("video-bytes")

	started := authenticatedJSON(t, s, user.ID, http.MethodPost, "/v1/uploads", map[string]any{
		"project_id":   project.ID,
		"filename":     "source.mp4",
		"content_type": "video/mp4",
		"size_bytes":   len(payload),
	})
	expectStatus(t, started, http.StatusCreated)
	upload := decodeResponse[uploadResponse](t, started)
	if upload.Mode != uploadSingle || upload.URL == nil {
		t.Fatalf("unexpected upload session: %+v", upload)
	}

	put, err := http.NewRequest(http.MethodPut, *upload.URL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build storage PUT: %v", err)
	}
	put.Header.Set("Content-Type", "video/mp4")
	put.Header.Set("Origin", "http://localhost:8000")
	response, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("direct storage PUT: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("storage PUT status = %d", response.StatusCode)
	}
	assertBrowserUploadHeaders(t, response)

	completed := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/uploads/"+upload.UploadID+"/complete", map[string]any{"parts": []any{}})
	expectStatus(t, completed, http.StatusAccepted)
	firstJob := decodeResponse[jobResponse](t, completed)

	repeated := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/uploads/"+upload.UploadID+"/complete", map[string]any{"parts": []any{}})
	expectStatus(t, repeated, http.StatusAccepted)
	secondJob := decodeResponse[jobResponse](t, repeated)
	if secondJob.ID != firstJob.ID {
		t.Fatalf("completion created duplicate jobs: %s and %s", firstJob.ID, secondJob.ID)
	}

	listed := authenticatedJSON(t, s, user.ID, http.MethodGet,
		"/v1/projects/"+project.ID+"/assets", nil)
	expectStatus(t, listed, http.StatusOK)
	sources := decodeResponse[assetListResponse](t, listed)
	if len(sources.Assets) != 1 || sources.Assets[0].ID != upload.AssetID {
		t.Fatalf("listed assets = %+v", sources.Assets)
	}
	if sources.Assets[0].Title != "source.mp4" || !sources.Assets[0].PreviewReady {
		t.Fatalf("upload source metadata = %+v", sources.Assets[0])
	}

	preview := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/assets/"+upload.AssetID+"/preview-url", nil)
	expectStatus(t, preview, http.StatusOK)
	signed := decodeResponse[presignedURLResponse](t, preview)
	get, err := http.Get(signed.URL)
	if err != nil {
		t.Fatalf("get source preview: %v", err)
	}
	got, readErr := io.ReadAll(get.Body)
	_ = get.Body.Close()
	if readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("preview bytes = %q, read error = %v", got, readErr)
	}

	deleted := authenticatedJSON(t, s, user.ID, http.MethodDelete, "/v1/projects/"+project.ID, nil)
	expectStatus(t, deleted, http.StatusNoContent)
}

func TestImportedSourceAppearsInProjectAndUsesTheSamePreview(t *testing.T) {
	s := newStorageTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	payload := []byte("downloaded-youtube-video")
	assetID := shared.NewID()
	objectKey := "source/" + user.ID + "/" + project.ID + "/" + assetID + "/original.mp4"
	sourceURL := "https://www.youtube.com/watch?v=test"

	if _, err := s.q.CreatePublicURLAsset(context.Background(), db.CreatePublicURLAssetParams{
		ID: assetID, ProjectID: project.ID, SourceUrl: &sourceURL, ObjectKey: objectKey,
	}); err != nil {
		t.Fatalf("create imported asset: %v", err)
	}
	putURL, _, err := s.presignSingleUpload(context.Background(), objectKey, "video/mp4")
	if err != nil {
		t.Fatalf("presign imported object: %v", err)
	}
	put, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build imported object PUT: %v", err)
	}
	put.Header.Set("Content-Type", "video/mp4")
	response, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("put imported object: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("imported object PUT status = %d", response.StatusCode)
	}

	contentType := "video/mp4"
	size := int64(len(payload))
	duration := int64(19_000)
	width, height := int32(1280), int32(720)
	checksum := "test-checksum"
	metadata := []byte(`{"title":"Downloaded YouTube source"}`)
	if _, err := s.q.CompleteIngestAsset(context.Background(), db.CompleteIngestAssetParams{
		ID: assetID, ProjectID: project.ID, ObjectKey: objectKey,
		ContentType: &contentType, SizeBytes: &size, DurationMs: &duration,
		Width: &width, Height: &height, Checksum: &checksum, Metadata: metadata,
	}); err != nil {
		t.Fatalf("complete imported asset: %v", err)
	}

	listed := authenticatedJSON(t, s, user.ID, http.MethodGet,
		"/v1/projects/"+project.ID+"/assets", nil)
	expectStatus(t, listed, http.StatusOK)
	sources := decodeResponse[assetListResponse](t, listed)
	if len(sources.Assets) != 1 {
		t.Fatalf("listed assets = %+v", sources.Assets)
	}
	asset := sources.Assets[0]
	if asset.SourceKind != "public_url" || asset.Title != "Downloaded YouTube source" ||
		asset.Status != "ready" || !asset.PreviewReady {
		t.Fatalf("imported source metadata = %+v", asset)
	}

	preview := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/assets/"+assetID+"/preview-url", nil)
	expectStatus(t, preview, http.StatusOK)
	signed := decodeResponse[presignedURLResponse](t, preview)
	get, err := http.Get(signed.URL)
	if err != nil {
		t.Fatalf("get imported preview: %v", err)
	}
	got, readErr := io.ReadAll(get.Body)
	_ = get.Body.Close()
	if readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("imported preview bytes = %q, read error = %v", got, readErr)
	}
}

func assertBrowserUploadHeaders(t *testing.T, response *http.Response) {
	t.Helper()
	if response.Header.Get("Access-Control-Allow-Origin") != "http://localhost:8000" {
		t.Fatalf("storage did not allow the Flutter Web origin: %q",
			response.Header.Get("Access-Control-Allow-Origin"))
	}
	exposed := strings.ToLower(response.Header.Get("Access-Control-Expose-Headers"))
	if exposed != "*" && !strings.Contains(exposed, "etag") {
		t.Fatalf("storage did not expose multipart ETag: %q", exposed)
	}
}

func TestMultipartSizingStaysInsideR2Limits(t *testing.T) {
	partSize, ok := multipartPartSize(maxObjectSize)
	if !ok {
		t.Fatal("maximum supported object did not produce a valid part size")
	}
	parts := countParts(maxObjectSize, partSize)
	if parts > maxMultipartParts {
		t.Fatalf("part count = %d, want at most %d", parts, maxMultipartParts)
	}
	if partSize < 5<<20 || partSize > maxPartSize {
		t.Fatalf("part size = %d, outside R2 limits", partSize)
	}
}
