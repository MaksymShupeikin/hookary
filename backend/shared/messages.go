package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/db"
)

const maxJobMessageBytes = 16 << 10

type JobMessage struct {
	JobID     string    `json:"job_id"`
	ProjectID string    `json:"project_id"`
	AssetID   *string   `json:"asset_id,omitempty"`
	Attempt   int32     `json:"attempt"`
	CreatedAt time.Time `json:"created_at"`
}

type JobEventError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type JobUpdatedEvent struct {
	JobID     string         `json:"job_id"`
	ProjectID string         `json:"project_id"`
	Stage     string         `json:"stage"`
	Status    string         `json:"status"`
	Progress  *float32       `json:"progress"`
	Attempt   int32          `json:"attempt"`
	Error     *JobEventError `json:"error,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type DeadLetterMessage struct {
	JobID     string        `json:"job_id,omitempty"`
	ProjectID string        `json:"project_id,omitempty"`
	Stage     string        `json:"stage"`
	Attempt   int32         `json:"attempt,omitempty"`
	Error     JobEventError `json:"error"`
	FailedAt  time.Time     `json:"failed_at"`
}

func NewJobMessage(job db.Job) JobMessage {
	return JobMessage{
		JobID: job.ID, ProjectID: job.ProjectID, AssetID: job.AssetID,
		Attempt: job.Attempt + 1, CreatedAt: job.CreatedAt.Time.UTC(),
	}
}

func NewJobUpdatedEvent(job db.Job) JobUpdatedEvent {
	var jobError *JobEventError
	if job.ErrorCode != nil {
		jobError = &JobEventError{Code: *job.ErrorCode}
		if job.ErrorMessage != nil {
			jobError.Message = *job.ErrorMessage
		}
	}
	return JobUpdatedEvent{
		JobID: job.ID, ProjectID: job.ProjectID, Stage: job.Stage,
		Status: job.Status, Progress: job.Progress, Attempt: job.Attempt,
		Error: jobError, CreatedAt: job.CreatedAt.Time.UTC(),
		UpdatedAt: job.UpdatedAt.Time.UTC(),
	}
}

func DecodeJobMessage(payload []byte) (JobMessage, error) {
	if len(payload) == 0 || len(payload) > maxJobMessageBytes {
		return JobMessage{}, errors.New("job message has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var message JobMessage
	if err := decoder.Decode(&message); err != nil {
		return JobMessage{}, fmt.Errorf("decode job message: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return JobMessage{}, errors.New("job message contains trailing JSON")
	}
	return message, nil
}

func (m JobMessage) Validate() error {
	if !ValidID(m.JobID) || !ValidID(m.ProjectID) {
		return errors.New("job message contains an invalid id")
	}
	if m.AssetID != nil && !ValidID(*m.AssetID) {
		return errors.New("job message contains an invalid asset id")
	}
	if m.Attempt < 1 || m.CreatedAt.IsZero() {
		return errors.New("job message contains invalid attempt metadata")
	}
	return nil
}
