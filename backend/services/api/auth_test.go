package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

// newTestServer builds a server backed by the real Postgres from the compose
// stack. Session rotation is transactional, recursive, and enforced by
// constraints, so a fake would only prove the fake works.
//
// Skips when TEST_DATABASE_URL is unset, which is how `go test ./...` stays
// runnable without Docker.
func newTestServer(t *testing.T) *server {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; run `make go-test-integration`")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	t.Cleanup(pool.Close)

	return &server{
		cfg:  testConfig(),
		log:  zerolog.Nop(),
		pool: pool,
		q:    db.New(pool),
	}
}

// createTestUser makes a user and removes it, and everything cascading from it,
// when the test ends.
func createTestUser(t *testing.T, s *server) db.User {
	t.Helper()

	id := shared.NewID()
	user, err := s.q.UpsertUserByGoogleSub(context.Background(), db.UpsertUserByGoogleSubParams{
		ID:          id,
		GoogleSub:   "google-sub-" + id,
		Email:       id + "@example.test",
		DisplayName: "Test User",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	t.Cleanup(func() {
		_, err := s.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
		if err != nil {
			t.Errorf("clean up user: %v", err)
		}
	})
	return user
}

func postJSON(t *testing.T, handler http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytesReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestSessionRotationRevokesTheOldSession(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	first, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	rec := postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *first.RefreshToken,
		Client:       clientMobile,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var second tokenPairResponse
	if err := json.NewDecoder(rec.Body).Decode(&second); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if second.RefreshToken == nil {
		t.Fatal("mobile refresh returned no token")
	}
	if *second.RefreshToken == *first.RefreshToken {
		t.Fatal("refresh returned the same token; it was not rotated")
	}

	old, err := s.q.GetSessionByTokenHash(context.Background(), hashRefreshToken(*first.RefreshToken))
	if err != nil {
		t.Fatalf("look up old session: %v", err)
	}
	if !old.RevokedAt.Valid {
		t.Error("the rotated-away session is still live")
	}
}

func TestReusedRefreshTokenKillsTheWholeChain(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	first, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	// The legitimate client refreshes.
	rec := postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *first.RefreshToken, Client: clientMobile,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh status = %d, want 200", rec.Code)
	}
	var second tokenPairResponse
	if err := json.NewDecoder(rec.Body).Decode(&second); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Someone replays the original token. It was rotated away, so only a leak
	// explains this.
	rec = postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *first.RefreshToken, Client: clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, want 401", rec.Code)
	}

	// The token the thief would have moved on to must now be dead too. If only
	// the replayed one were revoked, the attacker's newer session would survive.
	rec = postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *second.RefreshToken, Client: clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("descendant session still works after a replay: status = %d, want 401", rec.Code)
	}
}

func TestExpiredSessionCannotRefresh(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Auth.RefreshTokenTTL = -time.Hour
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	pair, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	rec := postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *pair.RefreshToken, Client: clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an expired session", rec.Code)
	}
}

func TestUnknownRefreshTokenIsRejected(t *testing.T) {
	s := newTestServer(t)

	rec := postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: "this-token-was-never-issued", Client: clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestWebRefreshUsesCookieAndWithholdsTokenFromBody(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	pair, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	// A browser sends the cookie and an empty body.
	refreshReq := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil)
	refreshReq.AddCookie(&http.Cookie{Name: refreshCookieName, Value: *pair.RefreshToken})
	rec := httptest.NewRecorder()
	s.handleRefresh(rec, refreshReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body tokenPairResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RefreshToken != nil {
		t.Error("web response carried the refresh token in the body, where JavaScript can read it")
	}
	if body.AccessToken == "" {
		t.Error("no access token returned")
	}

	var refreshCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == refreshCookieName {
			refreshCookie = c
		}
	}
	if refreshCookie == nil {
		t.Fatal("no refresh cookie was set")
	}
	if !refreshCookie.HttpOnly {
		t.Error("refresh cookie is not HttpOnly; an XSS could steal the session")
	}
	if refreshCookie.Path != refreshCookiePath {
		t.Errorf("cookie path = %q, want %q so it is not sent to every endpoint",
			refreshCookie.Path, refreshCookiePath)
	}
}

func TestLogoutRevokesTheSessionAndClearsTheCookie(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	pair, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	claims, err := s.parseAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	s.optionalAuth(http.HandlerFunc(s.handleLogout)).ServeHTTP(rec, logoutReq)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	session, err := s.q.GetSession(context.Background(), claims.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !session.RevokedAt.Valid {
		t.Error("session survived logout")
	}

	// The refresh token must not work afterwards.
	rec = postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *pair.RefreshToken, Client: clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout = %d, want 401", rec.Code)
	}
}

func TestLogoutWithoutTokenStillClearsTheCookie(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	rec := httptest.NewRecorder()
	s.optionalAuth(http.HandlerFunc(s.handleLogout)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == refreshCookieName && c.MaxAge >= 0 {
			t.Error("logout left the browser holding a refresh cookie")
		}
	}
}

func TestLogoutUsesRefreshTokenWhenAccessTokenIsUnavailable(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	pair, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	rec := postJSON(t, s.handleLogout, "/v1/auth/logout", refreshRequest{
		RefreshToken: *pair.RefreshToken,
		Client:       clientMobile,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}

	rec = postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *pair.RefreshToken,
		Client:       clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after refresh-token logout = %d, want 401", rec.Code)
	}
}

func TestWebLogoutUsesRefreshCookieWhenAccessTokenIsUnavailable(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	pair, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: refreshCookieName, Value: *pair.RefreshToken})
	rec := httptest.NewRecorder()
	s.handleLogout(rec, logoutReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}

	rec = postJSON(t, s.handleRefresh, "/v1/auth/refresh", refreshRequest{
		RefreshToken: *pair.RefreshToken,
		Client:       clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after cookie logout = %d, want 401", rec.Code)
	}
}

func TestMeRequiresAValidToken(t *testing.T) {
	s := newTestServer(t)
	user := createTestUser(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/google", nil)
	pair, err := s.startSession(req, user.ID, nil)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	handler := s.requireAuth(http.HandlerFunc(s.handleMe))

	// Without a token.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want 401", rec.Code)
	}

	// With one.
	authed := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	authed.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authed)

	if rec.Code != http.StatusOK {
		t.Fatalf("authed status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got userResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("id = %q, want %q", got.ID, user.ID)
	}
	if got.Email != user.Email {
		t.Errorf("email = %q, want %q", got.Email, user.Email)
	}
}

func TestGoogleAuthRejectsGarbageToken(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Auth.GoogleAudiences = []string{"test-client-id.apps.googleusercontent.com"}

	verifier, err := newGoogleVerifier(context.Background(), s.cfg.Auth)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	s.google = verifier

	rec := postJSON(t, s.handleGoogleAuth, "/v1/auth/google", googleAuthRequest{
		IDToken: "not.a.jwt",
		Client:  clientMobile,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestGoogleAuthIsDisabledWhenUnconfigured(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Auth.GoogleAudiences = nil

	rec := postJSON(t, s.handleGoogleAuth, "/v1/auth/google", googleAuthRequest{
		IDToken: "anything", Client: clientMobile,
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "auth_not_configured" {
		t.Errorf("code = %q, want auth_not_configured", body.Code)
	}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
