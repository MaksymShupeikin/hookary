package main

import (
	"encoding/json"
	"net/http"
)

// errorBody is the shape every failure returns. It matches the Error schema in
// contracts/openapi.yaml.
type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// writeJSON sends v as the response body.
//
// It encodes into a buffer first: encoding straight into the ResponseWriter
// commits a 200 before a marshal error can be reported, producing a truncated
// body under a success status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"internal_error","message":"Failed to encode response."}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeError sends a failure carrying the request id, so a user reporting an
// error hands over the exact string needed to find it in the logs.
//
// message is shown to people: say what happened, never leak internals.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, errorBody{
		Code:      code,
		Message:   message,
		RequestID: requestIDFrom(r.Context()),
	})
}
