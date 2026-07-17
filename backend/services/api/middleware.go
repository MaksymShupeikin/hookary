package main

import (
	"context"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type contextKey int

const (
	requestIDKey contextKey = iota
	claimsKey
)

// requestIDHeader is both read and written: a client or proxy that already has
// an id keeps it, so one id spans the whole hop.
const requestIDHeader = "X-Request-Id"

// withRequestID puts a request id on the context and echoes it back. Every log
// line and every error body carries it, which is what makes a user's report of
// "it failed" findable in the logs.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !shared.ValidID(id) {
			// Never trust a client-supplied id verbatim: it lands in logs, and
			// an arbitrary string there is a forgery and injection vector.
			id = shared.NewID()
		}

		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// claimsFrom returns the verified claims of the caller, or nil when the route
// did not require authentication.
func claimsFrom(ctx context.Context) *accessClaims {
	claims, _ := ctx.Value(claimsKey).(*accessClaims)
	return claims
}

// requireAuth rejects anything without a valid access token.
//
// It verifies the signature only and does not touch the database: that is the
// trade an access token buys. The cost is that revoking a session takes effect
// when the token expires rather than instantly, which is why the token is
// short-lived and the refresh token is the thing that gets revoked.
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "unauthorized",
				"This endpoint needs an access token.")
			return
		}

		claims, err := s.parseAccessToken(raw)
		if err != nil {
			// Debug, not warn: an expired token is the normal state of every
			// client between refreshes, and logging it at warn would bury the
			// real problems.
			s.log.Debug().
				Str("request_id", requestIDFrom(r.Context())).
				Err(err).
				Msg("access token rejected")
			writeError(w, r, http.StatusUnauthorized, "invalid_token",
				"That access token is not valid.")
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// optionalAuth attaches claims when a valid token is present and lets the
// request through either way. Logout uses it: signing out without a usable
// token must still clear the cookie rather than fail.
func (s *server) optionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := s.parseAccessToken(raw)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// withLogging logs one line per completed request.
func withLogging(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
				// Health checks run every few seconds forever and would drown
				// everything else.
				next.ServeHTTP(w, r)
				return
			}

			started := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(wrapped, r)

			event := logger.Info()
			if wrapped.Status() >= http.StatusInternalServerError {
				event = logger.Error()
			}

			event.
				Str("request_id", requestIDFrom(r.Context())).
				Str("method", r.Method).
				// Path only, never the raw URL: presigned URLs and tokens ride
				// in query strings and must not reach the logs.
				Str("path", r.URL.Path).
				Int("status", wrapped.Status()).
				Int("bytes", wrapped.BytesWritten()).
				Dur("took", time.Since(started)).
				Msg("request")
		})
	}
}

// withRecovery turns a panic into a 500 and keeps the process alive. One bad
// request must not take down the API for everyone else.
func withRecovery(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				// A hijacked connection has no response to write; re-panicking
				// lets the server tear it down.
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}

				logger.Error().
					Str("request_id", requestIDFrom(r.Context())).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Interface("panic", recovered).
					Bytes("stack", debug.Stack()).
					Msg("panic recovered")

				writeError(w, r, http.StatusInternalServerError,
					"internal_error", "Something went wrong on our side.")
			}()

			next.ServeHTTP(w, r)
		})
	}
}
