package main

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	s := testServer()
	userID, sessionID := shared.NewID(), shared.NewID()

	token, expires, err := s.mintAccessToken(userID, sessionID)
	if err != nil {
		t.Fatalf("mintAccessToken: %v", err)
	}

	claims, err := s.parseAccessToken(token)
	if err != nil {
		t.Fatalf("parseAccessToken: %v", err)
	}
	if claims.Subject != userID {
		t.Errorf("sub = %q, want %q", claims.Subject, userID)
	}
	if claims.SessionID != sessionID {
		t.Errorf("sid = %q, want %q", claims.SessionID, sessionID)
	}
	if got := time.Until(expires); got > 16*time.Minute {
		t.Errorf("token lives %v, want about the 15m TTL", got)
	}
}

func TestAccessTokenRejectsOtherSecret(t *testing.T) {
	issuer := testServer()
	token, _, err := issuer.mintAccessToken(shared.NewID(), shared.NewID())
	if err != nil {
		t.Fatalf("mintAccessToken: %v", err)
	}

	attacker := testServer()
	attacker.cfg.Auth.JWTSecret = []byte("a-completely-different-secret-value")

	if _, err := attacker.parseAccessToken(token); err == nil {
		t.Fatal("token signed with another secret was accepted")
	}
}

func TestAccessTokenRejectsAlgNone(t *testing.T) {
	s := testServer()

	// The classic JWT hole: a token that declares it needs no signature. If the
	// parser honours the header's alg, anyone can mint any identity.
	forged := jwt.NewWithClaims(jwt.SigningMethodNone, accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   shared.NewID(),
			Audience:  jwt.ClaimStrings{tokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		SessionID: shared.NewID(),
	})
	raw, err := forged.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build unsigned token: %v", err)
	}

	if _, err := s.parseAccessToken(raw); err == nil {
		t.Fatal("unsigned alg=none token was accepted")
	}
}

func TestAccessTokenRejectsExpired(t *testing.T) {
	s := testServer()
	s.cfg.Auth.AccessTokenTTL = -time.Minute

	token, _, err := s.mintAccessToken(shared.NewID(), shared.NewID())
	if err != nil {
		t.Fatalf("mintAccessToken: %v", err)
	}

	if _, err := s.parseAccessToken(token); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestAccessTokenRejectsForeignAudience(t *testing.T) {
	s := testServer()

	// A token signed with our secret but meant for something else must not
	// authenticate against this API.
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   shared.NewID(),
			Audience:  jwt.ClaimStrings{"some-other-service"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		SessionID: shared.NewID(),
	}).SignedString(s.cfg.Auth.JWTSecret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := s.parseAccessToken(raw); err == nil {
		t.Fatal("token for another audience was accepted")
	}
}

func TestRefreshTokensAreUniqueAndHashed(t *testing.T) {
	first, firstHash, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}
	second, secondHash, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}

	if first == second {
		t.Fatal("two refresh tokens came out identical")
	}
	if len(first) < 40 {
		t.Errorf("token is %d chars, want a 256-bit value", len(first))
	}
	if strings.Contains(string(firstHash), first) {
		t.Error("the hash contains the token verbatim")
	}
	if string(firstHash) == string(secondHash) {
		t.Error("different tokens hashed to the same value")
	}
	if got := string(hashRefreshToken(first)); got != string(firstHash) {
		t.Error("hashRefreshToken is not deterministic; lookups would miss")
	}
}
