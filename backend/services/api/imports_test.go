package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/MaksymShupeikin/hookary/backend/db"
)

func TestURLImportRejectsPrivateDestination(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)

	response := authenticatedJSON(t, s, user.ID, http.MethodPost, "/v1/imports", map[string]any{
		"project_id": project.ID,
		"url":        "http://169.254.169.254/latest/meta-data/",
	})
	expectStatus(t, response, http.StatusUnprocessableEntity)
}

func TestURLImportCreatesPublicAssetAndIngestJob(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)

	response := authenticatedJSON(t, s, user.ID, http.MethodPost, "/v1/imports", map[string]any{
		"project_id": project.ID,
		"url":        "https://1.1.1.1/public-video",
	})
	expectStatus(t, response, http.StatusAccepted)
	job := decodeResponse[jobResponse](t, response)
	if job.Stage != "ingest" || job.Status != "queued" {
		t.Fatalf("job = %+v", job)
	}

	storedJob, err := s.q.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if storedJob.AssetID == nil {
		t.Fatal("URL ingest job has no asset")
	}
	asset, err := s.q.GetAssetForIngest(context.Background(), db.GetAssetForIngestParams{
		ID: *storedJob.AssetID, ProjectID: project.ID,
	})
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if asset.SourceKind != "public_url" || asset.SourceUrl == nil || *asset.SourceUrl != "https://1.1.1.1/public-video" {
		t.Fatalf("asset = %+v", asset)
	}
}
