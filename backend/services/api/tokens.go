package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const (
	tokenIssuer   = "hookary"
	tokenAudience = "hookary-api"

	// 256 bits of entropy. The token is a password that grants a session, so it
	// has to be unguessable, not merely unique.
	refreshTokenBytes = 32
)

// accessClaims is what a Hookary access token carries. It is deliberately thin:
// the token is not a cache. Anything that can change — the user's name, their
// plan — is read from the database, so that a change takes effect now rather
// than whenever the token happens to expire.
type accessClaims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
}

// mintAccessToken issues a short-lived bearer token for a session.
func (s *server) mintAccessToken(userID, sessionID string) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(s.cfg.Auth.AccessTokenTTL)

	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   userID,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
			ID:        shared.NewID(),
		},
		SessionID: sessionID,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString(s.cfg.Auth.JWTSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expires, nil
}

// parseAccessToken verifies a token and returns its claims.
//
// The signing method is pinned to HS256. Without that pin, a token whose header
// says "alg":"none" would parse as valid, which is the oldest JWT hole there is.
func (s *server) parseAccessToken(raw string) (*accessClaims, error) {
	var claims accessClaims

	_, err := jwt.ParseWithClaims(raw, &claims,
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method %q", t.Header["alg"])
			}
			return s.cfg.Auth.JWTSecret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if claims.Subject == "" || claims.SessionID == "" {
		return nil, fmt.Errorf("token is missing sub or sid")
	}
	return &claims, nil
}

// newRefreshToken returns a fresh token and the hash to store for it.
//
// Only the hash is ever persisted, so a database dump yields no usable
// sessions. The hash doubles as the lookup key, which is also why no
// constant-time comparison is needed here: nothing is compared, it is indexed.
func newRefreshToken() (token string, hash []byte, err error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashRefreshToken(token), nil
}

// hashRefreshToken is a plain SHA-256: the input is 256 bits of uniform
// randomness, so there is nothing for a slow KDF to protect. Password hashing
// exists to survive guessable inputs, which this is not.
func hashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
