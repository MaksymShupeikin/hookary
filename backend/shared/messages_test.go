package shared

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecodeJobMessageRejectsUnknownAndTrailingJSON(t *testing.T) {
	valid := map[string]any{
		"job_id":     NewID(),
		"project_id": NewID(),
		"attempt":    1,
		"created_at": time.Now().UTC(),
	}
	payload, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeJobMessage(payload); err != nil {
		t.Fatalf("valid message: %v", err)
	}

	withUnknown := strings.TrimSuffix(string(payload), "}") + `,"video":"bytes"}`
	if _, err := DecodeJobMessage([]byte(withUnknown)); err == nil {
		t.Fatal("message with a payload field was accepted")
	}
	if _, err := DecodeJobMessage(append(payload, []byte(` {}`)...)); err == nil {
		t.Fatal("message with trailing JSON was accepted")
	}
}

func TestJobMessageValidationRequiresIdsAndAttempt(t *testing.T) {
	message := JobMessage{
		JobID: NewID(), ProjectID: NewID(), Attempt: 1, CreatedAt: time.Now(),
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("valid metadata: %v", err)
	}
	message.Attempt = 0
	if err := message.Validate(); err == nil {
		t.Fatal("zero attempt was accepted")
	}
}

func TestRetryDelayIsBoundedExponentialBackoff(t *testing.T) {
	wants := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for index, want := range wants {
		if got := RetryDelay(int32(index + 1)); got != want {
			t.Fatalf("RetryDelay(%d) = %s, want %s", index+1, got, want)
		}
	}
	if got := RetryDelay(100); got != time.Minute {
		t.Fatalf("large retry delay = %s, want one minute", got)
	}
}

func TestAutomaticStageOrderStopsAfterAnalysis(t *testing.T) {
	if nextPipelineStage("ingest") != "analyze" ||
		nextPipelineStage("analyze") != "" ||
		nextPipelineStage("render") != "" ||
		nextPipelineStage("publish") != "" {
		t.Fatal("rendering and publishing must require explicit user actions")
	}
}
