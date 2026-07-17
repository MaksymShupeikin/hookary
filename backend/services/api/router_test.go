package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func testConfig() shared.Config {
	return shared.Config{
		Env:                shared.EnvProd,
		LogLevel:           "error",
		CORSAllowedOrigins: []string{"https://app.hookary.com"},
		Auth: shared.AuthConfig{
			JWTSecret:       []byte("test-secret-that-is-long-enough-for-hs256"),
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 24 * time.Hour,
		},
	}
}

// testServer builds a server with no database. Routes that reach one are
// covered by the auth tests, which run against real Postgres.
func testServer() *server {
	return &server{cfg: testConfig(), log: zerolog.Nop()}
}

func testRouter(ready shared.ReadyFunc) http.Handler {
	return testServer().newRouter(ready)
}

func TestHealthLiveIgnoresDependencies(t *testing.T) {
	router := testRouter(func(context.Context) error {
		return errors.New("database is down")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	// A dead dependency means "stop sending traffic", not "restart me".
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHealthReadyReportsFailure(t *testing.T) {
	router := testRouter(func(context.Context) error {
		return errors.New("database is down")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestRequestIDIsGeneratedAndEchoed(t *testing.T) {
	router := testRouter(nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	got := rec.Header().Get(requestIDHeader)
	if !shared.ValidID(got) {
		t.Errorf("%s = %q, want a generated ULID", requestIDHeader, got)
	}
}

func TestRequestIDFromClientIsKept(t *testing.T) {
	router := testRouter(nil)
	incoming := shared.NewID()

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	req.Header.Set(requestIDHeader, incoming)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get(requestIDHeader); got != incoming {
		t.Errorf("%s = %q, want the client's id %q", requestIDHeader, got, incoming)
	}
}

func TestForgedRequestIDIsReplaced(t *testing.T) {
	router := testRouter(nil)
	forged := "not-a-ulid\nlevel=error msg=\"injected log line\""

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	req.Header.Set(requestIDHeader, forged)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	got := rec.Header().Get(requestIDHeader)
	if got == forged {
		t.Fatal("forged request id was echoed back; it reaches the logs verbatim")
	}
	if !shared.ValidID(got) {
		t.Errorf("%s = %q, want a generated ULID", requestIDHeader, got)
	}
}

func TestPanicBecomes500AndKeepsServing(t *testing.T) {
	router := chi.NewRouter()
	router.Use(withRequestID)
	router.Use(withRecovery(zerolog.Nop()))
	router.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	})
	router.Get("/fine", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", body.Code)
	}
	if body.RequestID == "" {
		t.Error("error body has no request_id; a user report cannot be traced without it")
	}
	if strings.Contains(body.Message, "handler exploded") {
		t.Errorf("panic text leaked to the client: %q", body.Message)
	}

	// The process must still serve other requests.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fine", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("after panic, status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	router := testRouter(nil)

	req := httptest.NewRequest(http.MethodOptions, "/health/live", nil)
	req.Header.Set("Origin", "https://app.hookary.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.hookary.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the configured origin", got)
	}
}

func TestCORSRejectsForeignOrigin(t *testing.T) {
	router := testRouter(nil)

	req := httptest.NewRequest(http.MethodOptions, "/health/live", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Credentials ride on these requests, so an unlisted origin must never be
	// echoed back.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for a foreign origin", got)
	}
}

func TestUnknownRouteReturnsJSONError(t *testing.T) {
	router := testRouter(nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code != "not_found" {
		t.Errorf("code = %q, want not_found", body.Code)
	}
}

func TestWriteJSONReportsUnencodableValue(t *testing.T) {
	rec := httptest.NewRecorder()

	// A channel cannot be marshalled. Encoding straight into the writer would
	// have already committed a 200 before this failed.
	writeJSON(rec, http.StatusOK, map[string]any{"bad": make(chan int)})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	body, _ := io.ReadAll(rec.Body)
	if !json.Valid(body) {
		t.Errorf("body is not valid JSON: %s", body)
	}
}
