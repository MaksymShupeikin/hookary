package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"google.golang.org/api/idtoken"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

// googleIssuers are the only issuers a Google ID token may claim.
var googleIssuers = []string{"accounts.google.com", "https://accounts.google.com"}

// errGoogleToken is returned for every rejected token. The specific reason is
// logged, never sent: telling a caller which check failed helps forge the next
// attempt and helps nobody else.
var errGoogleToken = errors.New("google id token rejected")

// googleIdentity is what a verified token tells us about a person.
type googleIdentity struct {
	Sub         string
	Email       string
	DisplayName string
	AvatarURL   string
}

// googleVerifier checks Google ID tokens.
//
// The validator fetches and caches Google's signing keys, so it is built once
// and reused; building one per request would re-fetch the key set on every
// login.
type googleVerifier struct {
	validator *idtoken.Validator
	audiences []string
}

func newGoogleVerifier(ctx context.Context, cfg shared.AuthConfig) (*googleVerifier, error) {
	validator, err := idtoken.NewValidator(ctx)
	if err != nil {
		return nil, fmt.Errorf("create google validator: %w", err)
	}
	return &googleVerifier{validator: validator, audiences: cfg.GoogleAudiences}, nil
}

// verify checks signature, issuer, audience, and expiry, and returns the
// identity the token asserts.
//
// The audience is checked here rather than passed to Validate, because a token
// may legitimately carry any of the per-platform client ids and Validate takes
// exactly one. Passing "" to Validate skips its own audience check, so this
// function must do it — an unchecked audience would accept a token minted for
// any other Google app on the internet.
func (v *googleVerifier) verify(ctx context.Context, rawToken string) (googleIdentity, error) {
	if len(v.audiences) == 0 {
		return googleIdentity{}, errors.New("no google audiences configured")
	}

	// Validate checks the signature and expiry against Google's key set.
	payload, err := v.validator.Validate(ctx, rawToken, "")
	if err != nil {
		return googleIdentity{}, fmt.Errorf("%w: %v", errGoogleToken, err)
	}

	if !slices.Contains(googleIssuers, payload.Issuer) {
		return googleIdentity{}, fmt.Errorf("%w: issuer %q", errGoogleToken, payload.Issuer)
	}
	if !slices.Contains(v.audiences, payload.Audience) {
		return googleIdentity{}, fmt.Errorf("%w: audience %q is not ours", errGoogleToken, payload.Audience)
	}
	if payload.Subject == "" {
		return googleIdentity{}, fmt.Errorf("%w: no subject", errGoogleToken)
	}

	// An unverified email must not become an account identity: it would let
	// someone claim an address they do not control.
	if verified, _ := payload.Claims["email_verified"].(bool); !verified {
		return googleIdentity{}, fmt.Errorf("%w: email is not verified", errGoogleToken)
	}

	email, _ := payload.Claims["email"].(string)
	if email == "" {
		return googleIdentity{}, fmt.Errorf("%w: no email", errGoogleToken)
	}

	name, _ := payload.Claims["name"].(string)
	if name == "" {
		name = email
	}
	picture, _ := payload.Claims["picture"].(string)

	return googleIdentity{
		Sub:         payload.Subject,
		Email:       email,
		DisplayName: name,
		AvatarURL:   picture,
	}, nil
}
