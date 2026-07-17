package shared

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment names. Dev turns on human-readable logs and is the only value
// that relaxes anything, so it must never be the fallback for an unset var.
const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

// Config is the whole configuration of any Hookary process, read once at
// startup. Services use the fields they need; there is no per-service struct,
// because five nearly identical config types would drift.
type Config struct {
	Env      string
	LogLevel string

	DatabaseURL      string
	DatabaseMaxConns int32

	NATSURL string

	S3 S3Config

	// Auth is read by every process but only meaningful to the API. It is not
	// validated by Load, so a worker with no JWT secret still starts; the API
	// calls Auth.Validate itself.
	Auth AuthConfig

	CORSAllowedOrigins []string
}

// AuthConfig covers Hookary's own sessions and the Google Sign-In it trusts.
type AuthConfig struct {
	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// GoogleAudiences are the OAuth client ids an ID token may be addressed to,
	// one per platform. A token for any other audience was minted for a
	// different app and must not log anyone in here.
	GoogleAudiences []string
}

// devJWTSecret is the placeholder in .env.example. It is public, so it must
// never be what signs a real session.
const devJWTSecret = "dev_only_insecure_secret"

// minJWTSecretLen is 32 bytes: below that, HS256 signatures are worth brute
// forcing.
const minJWTSecretLen = 32

// Validate reports whether auth is usable. Dev is allowed to run without Google
// credentials so the stack comes up before anyone has visited the Cloud
// console; prod is not.
func (a AuthConfig) Validate(env string) error {
	var problems []string

	switch {
	case len(a.JWTSecret) == 0:
		problems = append(problems, "JWT_SECRET is required but not set")
	case env == EnvProd && (string(a.JWTSecret) == devJWTSecret ||
		strings.HasPrefix(string(a.JWTSecret), "CHANGE_ME")):
		problems = append(problems, "JWT_SECRET is still the example value from .env.example")
	case env == EnvProd && len(a.JWTSecret) < minJWTSecretLen:
		problems = append(problems, fmt.Sprintf(
			"JWT_SECRET is %d bytes, want at least %d; generate one with: openssl rand -base64 48",
			len(a.JWTSecret), minJWTSecretLen))
	}

	if env == EnvProd && len(a.GoogleAudiences) == 0 {
		problems = append(problems,
			"no GOOGLE_CLIENT_ID_* is set, so no Google ID token could ever be accepted")
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid auth configuration:\n  - %s", strings.Join(problems, "\n  - "))
}

// GoogleConfigured reports whether Google Sign-In can work at all. The API
// starts without it and fails the auth endpoints instead, so that the rest of
// the stack is usable before the OAuth clients exist.
func (a AuthConfig) GoogleConfigured() bool { return len(a.GoogleAudiences) > 0 }

// S3Config points at Cloudflare R2 in production and MinIO locally. The two
// differ only in these values — no code branches on which one is behind them.
type S3Config struct {
	Endpoint        string
	PublicEndpoint  string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool

	// PresignTTL bounds how long a presigned URL is usable. It is a bearer
	// token, so this stays short.
	PresignTTL time.Duration
}

func (c Config) IsDev() bool { return c.Env == EnvDev }

// Load reads configuration from the environment and validates it.
//
// It reports every problem at once rather than the first one: a misconfigured
// deployment usually has several, and fixing them one restart at a time is
// miserable.
func Load() (Config, error) {
	var p envParser
	s3Endpoint := p.required("S3_ENDPOINT")

	cfg := Config{
		Env:      p.oneOf("HOOKARY_ENV", EnvDev, EnvDev, EnvProd),
		LogLevel: EnvOr("LOG_LEVEL", "info"),

		DatabaseURL:      p.required("DATABASE_URL"),
		DatabaseMaxConns: int32(p.intOr("DATABASE_MAX_CONNS", 10)),

		NATSURL: p.required("NATS_URL"),

		S3: S3Config{
			Endpoint:        s3Endpoint,
			PublicEndpoint:  EnvOr("S3_PUBLIC_ENDPOINT", s3Endpoint),
			Region:          EnvOr("S3_REGION", "auto"),
			Bucket:          p.required("S3_BUCKET"),
			AccessKeyID:     p.required("S3_ACCESS_KEY_ID"),
			SecretAccessKey: p.required("S3_SECRET_ACCESS_KEY"),
			ForcePathStyle:  p.boolOr("S3_FORCE_PATH_STYLE", false),
			PresignTTL:      p.durationOr("S3_PRESIGN_TTL", 15*time.Minute),
		},

		Auth: AuthConfig{
			JWTSecret:       []byte(os.Getenv("JWT_SECRET")),
			AccessTokenTTL:  p.durationOr("ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL: p.durationOr("REFRESH_TOKEN_TTL", 30*24*time.Hour),
			GoogleAudiences: googleAudiences(),
		},

		CORSAllowedOrigins: splitList(EnvOr("CORS_ALLOWED_ORIGINS", "")),
	}

	if err := p.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// EnvOr returns the value of key, or fallback when key is unset or empty.
// An empty variable counts as unset: compose passes through blank values for
// vars absent from .env, and a blank setting is never what we want.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// googleAudiences collects the per-platform client ids that are actually set.
// Unset ones are skipped rather than added as "", which would otherwise match
// a token with no audience.
func googleAudiences() []string {
	var out []string
	for _, key := range []string{
		"GOOGLE_CLIENT_ID_WEB",
		"GOOGLE_CLIENT_ID_IOS",
		"GOOGLE_CLIENT_ID_ANDROID",
	} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" && !strings.HasPrefix(v, "CHANGE_ME") {
			out = append(out, v)
		}
	}
	return out
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// envParser accumulates problems so Load can report them together.
type envParser struct {
	problems []string
}

func (p *envParser) fail(format string, args ...any) {
	p.problems = append(p.problems, fmt.Sprintf(format, args...))
}

func (p *envParser) err() error {
	if len(p.problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(p.problems, "\n  - "))
}

func (p *envParser) required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		p.fail("%s is required but not set", key)
	}
	return v
}

func (p *envParser) oneOf(key, fallback string, allowed ...string) string {
	v := EnvOr(key, fallback)
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	p.fail("%s is %q, want one of %s", key, v, strings.Join(allowed, ", "))
	return fallback
}

func (p *envParser) intOr(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		p.fail("%s is %q, want a number", key, raw)
		return fallback
	}
	return v
}

func (p *envParser) boolOr(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		p.fail("%s is %q, want true or false", key, raw)
		return fallback
	}
	return v
}

func (p *envParser) durationOr(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		p.fail("%s is %q, want a duration such as 15m", key, raw)
		return fallback
	}
	return v
}
