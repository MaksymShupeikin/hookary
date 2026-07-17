package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jackc/pgx/v5"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type websocketAuthRequest struct {
	Type        string `json:"type"`
	AccessToken string `json:"access_token"`
	ProjectID   string `json:"project_id"`
}

type websocketEnvelope struct {
	Type string        `json:"type"`
	Jobs []jobResponse `json:"jobs,omitempty"`
	Job  *jobResponse  `json:"job,omitempty"`
}

func (s *server) handleJobsWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.broker == nil {
		writeError(w, r, http.StatusServiceUnavailable, "jobs_unavailable", "Job updates are not available.")
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  s.cfg.CORSAllowedOrigins,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		s.log.Debug().Err(err).Msg("websocket upgrade rejected")
		return
	}
	defer connection.Close(websocket.StatusNormalClosure, "")
	connection.SetReadLimit(8 << 10)

	authCtx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	var auth websocketAuthRequest
	if err := wsjson.Read(authCtx, connection, &auth); err != nil {
		_ = connection.Close(websocket.StatusPolicyViolation, "authentication required")
		return
	}
	claims, err := s.parseAccessToken(auth.AccessToken)
	if err != nil || auth.Type != "authenticate" || !shared.ValidID(auth.ProjectID) {
		_ = connection.Close(websocket.StatusPolicyViolation, "authentication failed")
		return
	}
	if _, err := s.q.GetProject(s.ctx, db.GetProjectParams{
		ID: auth.ProjectID, UserID: claims.Subject,
	}); errors.Is(err, pgx.ErrNoRows) {
		_ = connection.Close(websocket.StatusPolicyViolation, "project not found")
		return
	} else if err != nil {
		s.log.Error().Err(err).Msg("authorize websocket project")
		_ = connection.Close(websocket.StatusInternalError, "authorization failed")
		return
	}

	events, unsubscribe := s.broker.subscribe(auth.ProjectID)
	defer unsubscribe()
	jobs, err := s.q.ListOwnedProjectJobs(s.ctx, db.ListOwnedProjectJobsParams{
		ProjectID: auth.ProjectID, UserID: claims.Subject,
	})
	if err != nil {
		s.log.Error().Err(err).Msg("load websocket job snapshot")
		_ = connection.Close(websocket.StatusInternalError, "snapshot failed")
		return
	}
	snapshot := make([]jobResponse, 0, len(jobs))
	for _, job := range jobs {
		snapshot = append(snapshot, jobToResponse(job))
	}
	if err := writeWebSocket(connection, websocketEnvelope{Type: "snapshot", Jobs: snapshot}); err != nil {
		return
	}

	closed := connection.CloseRead(s.ctx)
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-s.ctx.Done():
			_ = connection.Close(websocket.StatusGoingAway, "server shutting down")
			return
		case <-closed.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			job := eventToJobResponse(event)
			if err := writeWebSocket(connection, websocketEnvelope{Type: "job_updated", Job: &job}); err != nil {
				return
			}
		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
			err := connection.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func writeWebSocket(connection *websocket.Conn, value websocketEnvelope) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, connection, value)
}

func eventToJobResponse(event shared.JobUpdatedEvent) jobResponse {
	var jobError *errorBody
	if event.Error != nil {
		jobError = &errorBody{Code: event.Error.Code, Message: event.Error.Message}
	}
	return jobResponse{
		ID: event.JobID, ProjectID: event.ProjectID, Stage: event.Stage,
		Status: event.Status, Progress: event.Progress, Attempt: event.Attempt,
		Error:     jobError,
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: event.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
