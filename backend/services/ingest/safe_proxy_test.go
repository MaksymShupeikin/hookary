package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSafeProxyRejectsLoopbackDestination(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/video", nil)
	(&safeProxy{}).serveHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}
