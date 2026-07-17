package main

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func createJobForTest(t *testing.T, s *server, projectID string) db.Job {
	t.Helper()
	key := "test-ingest:" + shared.NewID()
	job, err := s.q.CreateIngestJob(context.Background(), db.CreateIngestJobParams{
		ID: shared.NewID(), ProjectID: projectID, IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	return job
}

func TestJobsAreScopedAndListedByProject(t *testing.T) {
	s := newTestServer(t)
	owner := createTestUser(t, s)
	other := createTestUser(t, s)
	project := createProjectForTest(t, s, owner.ID)
	job := createJobForTest(t, s, project.ID)

	got := authenticatedJSON(t, s, owner.ID, http.MethodGet, "/v1/jobs/"+job.ID, nil)
	expectStatus(t, got, http.StatusOK)
	if response := decodeResponse[jobResponse](t, got); response.ID != job.ID {
		t.Fatalf("job id = %s, want %s", response.ID, job.ID)
	}

	listed := authenticatedJSON(t, s, owner.ID, http.MethodGet,
		"/v1/projects/"+project.ID+"/jobs", nil)
	expectStatus(t, listed, http.StatusOK)
	jobs := decodeResponse[jobListResponse](t, listed)
	if len(jobs.Jobs) != 1 || jobs.Jobs[0].ID != job.ID {
		t.Fatalf("listed jobs = %+v", jobs.Jobs)
	}

	denied := authenticatedJSON(t, s, other.ID, http.MethodGet, "/v1/jobs/"+job.ID, nil)
	expectStatus(t, denied, http.StatusNotFound)
}

func TestClaimAndNextStageAreIdempotent(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	job := createJobForTest(t, s, project.ID)

	claimed, err := s.q.ClaimJob(context.Background(), db.ClaimJobParams{
		JobID: job.ID, Stage: "ingest", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed.Status != "running" || claimed.Attempt != 1 {
		t.Fatalf("claimed job = %+v", claimed)
	}
	if _, err := s.q.ClaimJob(context.Background(), db.ClaimJobParams{
		JobID: job.ID, Stage: "ingest", Attempt: 1,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate claim error = %v, want no rows", err)
	}

	if _, err := s.q.SucceedJob(context.Background(), db.SucceedJobParams{
		ID: job.ID, Attempt: 1,
	}); err != nil {
		t.Fatalf("succeed job: %v", err)
	}
	key := "analyze:v1:" + job.ID
	first, err := s.q.CreateNextStageJob(context.Background(), db.CreateNextStageJobParams{
		ID: shared.NewID(), ProjectID: project.ID, Stage: "analyze", IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("create next stage: %v", err)
	}
	second, err := s.q.CreateNextStageJob(context.Background(), db.CreateNextStageJobParams{
		ID: shared.NewID(), ProjectID: project.ID, Stage: "analyze", IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("repeat next stage: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate next jobs = %s and %s", first.ID, second.ID)
	}
}

func TestFailedJobCanBeRetriedOnceItIsOwned(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	job := createJobForTest(t, s, project.ID)
	if _, err := s.pool.Exec(context.Background(),
		"UPDATE jobs SET status = 'failed', attempt = 5 WHERE id = $1", job.ID); err != nil {
		t.Fatalf("mark job failed: %v", err)
	}

	retried := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/jobs/"+job.ID+"/retry", nil)
	expectStatus(t, retried, http.StatusAccepted)
	response := decodeResponse[jobResponse](t, retried)
	if response.Status != "queued" || response.Attempt != 5 {
		t.Fatalf("retried job = %+v", response)
	}

	again := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/jobs/"+job.ID+"/retry", nil)
	expectStatus(t, again, http.StatusConflict)
}
