package main

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type jobListResponse struct {
	Jobs []jobResponse `json:"jobs"`
}

func (s *server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID, ok := pathID(w, r, "job_id")
	if !ok {
		return
	}
	job, err := s.q.GetOwnedJob(r.Context(), db.GetOwnedJobParams{
		ID: jobID, UserID: claimsFrom(r.Context()).Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such job.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get job")
		return
	}
	writeJSON(w, http.StatusOK, jobToResponse(job))
}

func (s *server) handleListProjectJobs(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "project_id")
	if !ok {
		return
	}
	jobs, err := s.q.ListOwnedProjectJobs(r.Context(), db.ListOwnedProjectJobsParams{
		ProjectID: projectID, UserID: claimsFrom(r.Context()).Subject,
	})
	if err != nil {
		s.fail(w, r, err, "list project jobs")
		return
	}
	// An empty list is ambiguous without checking the project: it can mean a
	// new project or somebody else's project, and ownership must not leak.
	if len(jobs) == 0 {
		if _, err := s.q.GetProject(r.Context(), db.GetProjectParams{
			ID: projectID, UserID: claimsFrom(r.Context()).Subject,
		}); errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "not_found", "No such project.")
			return
		} else if err != nil {
			s.fail(w, r, err, "get project for jobs")
			return
		}
	}
	responses := make([]jobResponse, 0, len(jobs))
	for _, job := range jobs {
		responses = append(responses, jobToResponse(job))
	}
	writeJSON(w, http.StatusOK, jobListResponse{Jobs: responses})
}

func (s *server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	jobID, ok := pathID(w, r, "job_id")
	if !ok {
		return
	}
	userID := claimsFrom(r.Context()).Subject
	current, err := s.q.GetOwnedJob(r.Context(), db.GetOwnedJobParams{ID: jobID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such job.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get job for retry")
		return
	}
	if current.Status != "failed" {
		writeError(w, r, http.StatusConflict, "job_not_retryable", "Only a failed job can be retried.")
		return
	}

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.fail(w, r, err, "begin retry transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := s.q.WithTx(tx)
	job, err := q.RetryOwnedJob(r.Context(), db.RetryOwnedJobParams{ID: jobID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusConflict, "job_not_retryable", "Only a failed job can be retried.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "retry job")
		return
	}
	if err := q.SetProjectProcessing(r.Context(), db.SetProjectProcessingParams{
		ID: job.ProjectID, UserID: userID,
	}); err != nil {
		s.fail(w, r, err, "mark retried project processing")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.fail(w, r, err, "commit retry transaction")
		return
	}
	shared.PublishJobEvent(s.nats, job, s.log)
	writeJSON(w, http.StatusAccepted, jobToResponse(job))
}
