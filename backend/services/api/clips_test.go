package main

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func createSuggestedClipForTest(
	t *testing.T,
	s *server,
	projectID string,
) db.Clip {
	t.Helper()
	assetID := shared.NewID()
	sourceURL := "https://example.test/video"
	if _, err := s.q.CreatePublicURLAsset(context.Background(), db.CreatePublicURLAssetParams{
		ID: assetID, ProjectID: projectID, SourceUrl: &sourceURL,
		ObjectKey: "source/test/" + assetID + "/original.mp4",
	}); err != nil {
		t.Fatalf("create clip asset: %v", err)
	}
	clip, err := s.q.CreateClip(context.Background(), db.CreateClipParams{
		ID: shared.NewID(), ProjectID: projectID, AssetID: assetID,
		Title: "Selected moment", Score: 0.9, StartMs: 10_000, EndMs: 40_000,
	})
	if err != nil {
		t.Fatalf("create suggested clip: %v", err)
	}
	return clip
}

func TestSelectedClipsQueueIdempotentRenderJobs(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	clip := createSuggestedClipForTest(t, s, project.ID)

	queued := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/projects/"+project.ID+"/clips/render",
		map[string]any{"clip_ids": []string{clip.ID}})
	expectStatus(t, queued, http.StatusAccepted)
	jobs := decodeResponse[renderClipsResponse](t, queued)
	if len(jobs.Jobs) != 1 || jobs.Jobs[0].Stage != "render" {
		t.Fatalf("render jobs = %+v", jobs.Jobs)
	}

	repeated := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/projects/"+project.ID+"/clips/render",
		map[string]any{"clip_ids": []string{clip.ID}})
	expectStatus(t, repeated, http.StatusAccepted)
	again := decodeResponse[renderClipsResponse](t, repeated)
	if again.Jobs[0].ID != jobs.Jobs[0].ID {
		t.Fatalf("duplicate render jobs = %s and %s", jobs.Jobs[0].ID, again.Jobs[0].ID)
	}

	stored, err := s.q.GetClipForRender(context.Background(), db.GetClipForRenderParams{
		ID: clip.ID, ProjectID: project.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "rendering" {
		t.Fatalf("clip status = %s, want rendering", stored.Status)
	}
}

func TestOlderRenderedClipCanQueueCurrentRevision(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	clip := createSuggestedClipForTest(t, s, project.ID)
	oldKey := "output/" + user.ID + "/" + project.ID + "/" + clip.ID + "/v1.mp4"
	if _, err := s.pool.Exec(context.Background(),
		"UPDATE clips SET status = 'rendered', output_key = $2 WHERE id = $1",
		clip.ID, oldKey); err != nil {
		t.Fatalf("mark old clip rendered: %v", err)
	}

	response := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/projects/"+project.ID+"/clips/render",
		map[string]any{"clip_ids": []string{clip.ID}})
	expectStatus(t, response, http.StatusAccepted)

	stored, err := s.q.GetClipForRender(context.Background(), db.GetClipForRenderParams{
		ID: clip.ID, ProjectID: project.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "rendering" || stored.OutputKey == nil || *stored.OutputKey != oldKey {
		t.Fatalf("clip after upgrade queue = %+v", stored)
	}
}

func TestCurrentRenderedClipCannotQueueDuplicateRevision(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	clip := createSuggestedClipForTest(t, s, project.ID)
	currentKey := "output/" + user.ID + "/" + project.ID + "/" + clip.ID + "/v4.mp4"
	if _, err := s.pool.Exec(context.Background(),
		"UPDATE clips SET status = 'rendered', output_key = $2 WHERE id = $1",
		clip.ID, currentKey); err != nil {
		t.Fatalf("mark current clip rendered: %v", err)
	}

	response := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/projects/"+project.ID+"/clips/render",
		map[string]any{"clip_ids": []string{clip.ID}})
	expectStatus(t, response, http.StatusConflict)
}

func TestRenderedClipDownloadIsShortLivedAndOwned(t *testing.T) {
	s := newStorageTestServer(t)
	owner := createTestUser(t, s)
	other := createTestUser(t, s)
	project := createProjectForTest(t, s, owner.ID)
	clip := createSuggestedClipForTest(t, s, project.ID)
	outputKey := "output/" + owner.ID + "/" + project.ID + "/" + clip.ID + "/v1.mp4"
	video := []byte("rendered-video")
	if _, err := s.storage.Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(outputKey),
		Body: bytes.NewReader(video), ContentLength: aws.Int64(int64(len(video))),
		ContentType: aws.String("video/mp4"),
	}); err != nil {
		t.Fatalf("put rendered clip: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.storage.Client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
			Bucket: aws.String(s.storage.Bucket), Key: aws.String(outputKey),
		})
	})
	if _, err := s.pool.Exec(context.Background(),
		"UPDATE clips SET status = 'rendered', output_key = $2 WHERE id = $1",
		clip.ID, outputKey); err != nil {
		t.Fatalf("mark clip rendered: %v", err)
	}

	denied := authenticatedJSON(t, s, other.ID, http.MethodPost,
		"/v1/clips/"+clip.ID+"/download-url", nil)
	expectStatus(t, denied, http.StatusNotFound)

	response := authenticatedJSON(t, s, owner.ID, http.MethodPost,
		"/v1/clips/"+clip.ID+"/download-url", nil)
	expectStatus(t, response, http.StatusOK)
	download := decodeResponse[clipDownloadResponse](t, response)
	if download.URL == "" || download.ExpiresAt == "" ||
		download.Filename != "hookary-"+clip.ID+".mp4" {
		t.Fatalf("download response = %+v", download)
	}

	previewResponse := authenticatedJSON(t, s, owner.ID, http.MethodPost,
		"/v1/clips/"+clip.ID+"/preview-url", nil)
	expectStatus(t, previewResponse, http.StatusOK)
	preview := decodeResponse[clipPreviewResponse](t, previewResponse)
	if preview.URL == "" || preview.ExpiresAt == "" {
		t.Fatalf("preview response = %+v", preview)
	}
}
