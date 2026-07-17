package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

// refreshCookieName carries the refresh token on web.
//
// The path scopes it to the auth endpoints, so it is not attached to every
// other API call. A token that rides along on requests that do not need it is
// a token with more chances to leak.
const (
	refreshCookieName = "hookary_refresh"
	refreshCookiePath = "/v1/auth"
)

// clientKind decides how the refresh token is delivered. Browsers must never
// see it in a response body — JavaScript could read it, and an XSS would walk
// away with a long-lived session. Native apps have no cookie jar and keep it in
// the platform keystore instead.
const (
	clientWeb    = "web"
	clientMobile = "mobile"
)

type googleAuthRequest struct {
	IDToken string `json:"id_token"`
	Client  string `json:"client_kind"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
	Client       string `json:"client_kind"`
}

type tokenPairResponse struct {
	AccessToken  string  `json:"access_token"`
	ExpiresIn    int     `json:"expires_in"`
	RefreshToken *string `json:"refresh_token"`
}

type userResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	CreatedAt   string  `json:"created_at"`
}

func userToResponse(u db.User) userResponse {
	return userResponse{
		ID:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarUrl,
		CreatedAt:   u.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// handleGoogleAuth exchanges a Google ID token for a Hookary session.
func (s *server) handleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Auth.GoogleConfigured() {
		writeError(w, r, http.StatusServiceUnavailable, "auth_not_configured",
			"Google Sign-In is not configured on this server.")
		return
	}

	var req googleAuthRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.IDToken == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "id_token is required.")
		return
	}
	client, ok := parseClientKind(w, r, req.Client)
	if !ok {
		return
	}

	identity, err := s.google.verify(r.Context(), req.IDToken)
	if err != nil {
		s.log.Warn().
			Str("request_id", requestIDFrom(r.Context())).
			Err(err).
			Msg("google id token rejected")
		writeError(w, r, http.StatusUnauthorized, "invalid_token",
			"That Google sign-in could not be verified.")
		return
	}

	user, err := s.q.UpsertUserByGoogleSub(r.Context(), db.UpsertUserByGoogleSubParams{
		ID:          shared.NewID(),
		GoogleSub:   identity.Sub,
		Email:       identity.Email,
		DisplayName: identity.DisplayName,
		AvatarUrl:   emptyToNil(identity.AvatarURL),
	})
	if err != nil {
		s.fail(w, r, err, "upsert user")
		return
	}

	pair, err := s.startSession(r, user.ID, nil)
	if err != nil {
		s.fail(w, r, err, "create session")
		return
	}

	s.log.Info().
		Str("request_id", requestIDFrom(r.Context())).
		Str("user_id", user.ID).
		Msg("signed in with google")

	s.respondWithTokens(w, r, pair, client, http.StatusOK)
}

// handleRefresh rotates the refresh token.
//
// Every refresh mints a new token and revokes the old one, so a stolen token is
// only useful until the real client next refreshes — at which point the theft
// becomes visible below.
func (s *server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	// An empty body is normal on web, where the token is in a cookie.
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}

	token, client := s.readRefreshToken(r, req)
	if token == "" {
		writeError(w, r, http.StatusUnauthorized, "invalid_token", "No refresh token was sent.")
		return
	}

	session, err := s.q.GetSessionByTokenHash(r.Context(), hashRefreshToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusUnauthorized, "invalid_token", "That session is no longer valid.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "look up session")
		return
	}

	if session.RevokedAt.Valid {
		// This token was already rotated away, so it should not exist anywhere
		// but in an attacker's hands. Whoever holds it, the safe move is to
		// kill the whole chain and make both parties sign in again.
		s.log.Warn().
			Str("request_id", requestIDFrom(r.Context())).
			Str("user_id", session.UserID).
			Str("session_id", session.ID).
			Msg("revoked refresh token reused; revoking session tree")

		if err := s.q.RevokeSessionTree(r.Context(), session.ID); err != nil {
			s.fail(w, r, err, "revoke session tree")
			return
		}
		s.clearRefreshCookie(w)
		writeError(w, r, http.StatusUnauthorized, "invalid_token", "That session is no longer valid.")
		return
	}

	if time.Now().After(session.ExpiresAt.Time) {
		writeError(w, r, http.StatusUnauthorized, "invalid_token", "That session has expired.")
		return
	}

	pair, err := s.rotateSession(r, session)
	if err != nil {
		s.fail(w, r, err, "rotate session")
		return
	}

	s.respondWithTokens(w, r, pair, client, http.StatusOK)
}

// handleLogout revokes the caller's session.
func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear the cookie no matter what follows: a logout must never leave the
	// browser holding a token, even if the session was already gone.
	s.clearRefreshCookie(w)

	var req refreshRequest
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}

	sessionID := ""
	if claims := claimsFrom(r.Context()); claims != nil {
		sessionID = claims.SessionID
	} else if token, _ := s.readRefreshToken(r, req); token != "" {
		session, err := s.q.GetSessionByTokenHash(r.Context(), hashRefreshToken(token))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			s.fail(w, r, err, "look up session for logout")
			return
		}
		if err == nil {
			sessionID = session.ID
		}
	}

	if sessionID != "" {
		if err := s.q.RevokeSession(r.Context(), sessionID); err != nil {
			s.fail(w, r, err, "revoke session")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the signed-in user.
func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r.Context())

	user, err := s.q.GetUser(r.Context(), claims.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		// The token is valid but its user is gone.
		writeError(w, r, http.StatusUnauthorized, "invalid_token", "That account no longer exists.")
		return
	}
	if err != nil {
		s.fail(w, r, err, "get user")
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

// startSession creates the first session of a login chain.
func (s *server) startSession(r *http.Request, userID string, rotatedFrom *string) (tokenPairResponse, error) {
	token, hash, err := newRefreshToken()
	if err != nil {
		return tokenPairResponse{}, err
	}

	session, err := s.q.CreateSession(r.Context(), db.CreateSessionParams{
		ID:               shared.NewID(),
		UserID:           userID,
		RefreshTokenHash: hash,
		RotatedFrom:      rotatedFrom,
		UserAgent:        emptyToNil(truncate(r.UserAgent(), 500)),
		Ip:               clientIP(r),
		ExpiresAt:        timestamptz(time.Now().Add(s.cfg.Auth.RefreshTokenTTL)),
	})
	if err != nil {
		return tokenPairResponse{}, err
	}

	access, expires, err := s.mintAccessToken(userID, session.ID)
	if err != nil {
		return tokenPairResponse{}, err
	}

	return tokenPairResponse{
		AccessToken:  access,
		ExpiresIn:    int(time.Until(expires).Seconds()),
		RefreshToken: &token,
	}, nil
}

// rotateSession replaces a session with a fresh one and revokes the old.
//
// Both writes happen in one transaction: a crash between them would either
// leave two live sessions or none, and "none" logs the user out for no reason.
func (s *server) rotateSession(r *http.Request, old db.Session) (tokenPairResponse, error) {
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return tokenPairResponse{}, err
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	qtx := s.q.WithTx(tx)

	token, hash, err := newRefreshToken()
	if err != nil {
		return tokenPairResponse{}, err
	}

	session, err := qtx.CreateSession(r.Context(), db.CreateSessionParams{
		ID:               shared.NewID(),
		UserID:           old.UserID,
		RefreshTokenHash: hash,
		RotatedFrom:      &old.ID,
		UserAgent:        emptyToNil(truncate(r.UserAgent(), 500)),
		Ip:               clientIP(r),
		ExpiresAt:        timestamptz(time.Now().Add(s.cfg.Auth.RefreshTokenTTL)),
	})
	if err != nil {
		return tokenPairResponse{}, err
	}

	if err := qtx.RevokeSession(r.Context(), old.ID); err != nil {
		return tokenPairResponse{}, err
	}

	if err := tx.Commit(r.Context()); err != nil {
		return tokenPairResponse{}, err
	}

	access, expires, err := s.mintAccessToken(old.UserID, session.ID)
	if err != nil {
		return tokenPairResponse{}, err
	}

	return tokenPairResponse{
		AccessToken:  access,
		ExpiresIn:    int(time.Until(expires).Seconds()),
		RefreshToken: &token,
	}, nil
}

// respondWithTokens delivers the pair the way the client can safely hold it.
func (s *server) respondWithTokens(w http.ResponseWriter, r *http.Request, pair tokenPairResponse, client string, status int) {
	if client == clientWeb {
		s.setRefreshCookie(w, *pair.RefreshToken)
		// Withheld from the body on purpose: in a browser the cookie is the
		// only copy, and it is one JavaScript cannot reach.
		pair.RefreshToken = nil
	}
	writeJSON(w, status, pair)
}

func (s *server) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		MaxAge:   int(s.cfg.Auth.RefreshTokenTTL.Seconds()),
		HttpOnly: true,
		// Secure would make the cookie undeliverable over plain http, which is
		// what local development runs on.
		Secure:   !s.cfg.IsDev(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !s.cfg.IsDev(),
		SameSite: http.SameSiteLaxMode,
	})
}

// readRefreshToken takes the token from wherever this client keeps it. The
// cookie wins: if one is present the caller is a browser, whatever the body
// claims.
func (s *server) readRefreshToken(r *http.Request, req refreshRequest) (token, client string) {
	if cookie, err := r.Cookie(refreshCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, clientWeb
	}
	return req.RefreshToken, clientMobile
}

func parseClientKind(w http.ResponseWriter, r *http.Request, kind string) (string, bool) {
	switch kind {
	case clientWeb, clientMobile:
		return kind, true
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_request",
			`client_kind must be "web" or "mobile".`)
		return "", false
	}
}

// decodeJSON reads the body, or answers 400 and reports false.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	// A body this large is not a client of ours. Without the cap, a request can
	// make the server allocate until it dies.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Malformed request body.")
		return false
	}
	return true
}

// fail logs the real error and returns a generic one. The internals of a
// failure are for us, not for whoever sent the request.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error, what string) {
	s.log.Error().
		Str("request_id", requestIDFrom(r.Context())).
		Err(err).
		Msg(what)
	writeError(w, r, http.StatusInternalServerError, "internal_error",
		"Something went wrong on our side.")
}

func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// clientIP records who a session was created from. It reads RemoteAddr only:
// X-Forwarded-For is caller-controlled, and trusting it before a proxy is
// actually in front of us would just record whatever a client made up.
func clientIP(r *http.Request) *netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return nil
	}
	return &addr
}
