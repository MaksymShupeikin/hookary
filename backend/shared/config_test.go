package shared

import (
	"strings"
	"testing"
	"time"
)

// setValidEnv sets the minimum required for Load to succeed. Tests then break
// exactly one thing.
func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("NATS_URL", "nats://localhost:4222")
	t.Setenv("S3_ENDPOINT", "http://localhost:9000")
	t.Setenv("S3_BUCKET", "hookary-media-test")
	t.Setenv("S3_ACCESS_KEY_ID", "key")
	t.Setenv("S3_SECRET_ACCESS_KEY", "secret")
}

func TestLoadDefaults(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want no error", err)
	}

	if cfg.Env != EnvDev {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvDev)
	}
	if cfg.DatabaseMaxConns != 10 {
		t.Errorf("DatabaseMaxConns = %d, want 10", cfg.DatabaseMaxConns)
	}
	if cfg.S3.PresignTTL != 15*time.Minute {
		t.Errorf("PresignTTL = %v, want 15m", cfg.S3.PresignTTL)
	}
	if cfg.S3.Region != "auto" {
		t.Errorf("Region = %q, want auto", cfg.S3.Region)
	}
	if cfg.S3.PublicEndpoint != cfg.S3.Endpoint {
		t.Errorf("PublicEndpoint = %q, want endpoint fallback %q", cfg.S3.PublicEndpoint, cfg.S3.Endpoint)
	}
}

func TestLoadReportsEveryMissingVarAtOnce(t *testing.T) {
	// Nothing set. A deployment usually has several problems, and surfacing
	// them one restart at a time is the thing this avoids.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("NATS_URL", "")
	t.Setenv("S3_ENDPOINT", "")
	t.Setenv("S3_BUCKET", "")
	t.Setenv("S3_ACCESS_KEY_ID", "")
	t.Setenv("S3_SECRET_ACCESS_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() = nil, want an error")
	}

	for _, key := range []string{
		"DATABASE_URL", "NATS_URL", "S3_ENDPOINT",
		"S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY",
	} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s:\n%v", key, err)
		}
	}
}

func TestLoadRejectsUnknownEnv(t *testing.T) {
	setValidEnv(t)
	t.Setenv("HOOKARY_ENV", "staging")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() = nil, want an error for an unknown environment")
	}
	if !strings.Contains(err.Error(), "HOOKARY_ENV") {
		t.Errorf("error does not mention HOOKARY_ENV:\n%v", err)
	}
}

func TestLoadRejectsMalformedDuration(t *testing.T) {
	setValidEnv(t)
	t.Setenv("S3_PRESIGN_TTL", "fifteen minutes")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() = nil, want an error for a malformed duration")
	}
}

func TestLoadParsesCORSOrigins(t *testing.T) {
	setValidEnv(t)
	// Spaces around entries are normal in a hand-edited .env.
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:8000, https://app.hookary.com ,")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want no error", err)
	}

	want := []string{"http://localhost:8000", "https://app.hookary.com"}
	if len(cfg.CORSAllowedOrigins) != len(want) {
		t.Fatalf("CORSAllowedOrigins = %q, want %q", cfg.CORSAllowedOrigins, want)
	}
	for i := range want {
		if cfg.CORSAllowedOrigins[i] != want[i] {
			t.Errorf("origin[%d] = %q, want %q", i, cfg.CORSAllowedOrigins[i], want[i])
		}
	}
}

func TestProdIsNotDev(t *testing.T) {
	setValidEnv(t)
	t.Setenv("HOOKARY_ENV", EnvProd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want no error", err)
	}
	if cfg.IsDev() {
		t.Error("IsDev() = true for prod; dev relaxes behaviour and must never be inferred")
	}
}

func TestProdRejectsExampleJWTSecret(t *testing.T) {
	for _, secret := range []string{devJWTSecret, "CHANGE_ME_openssl_rand_base64_48"} {
		t.Run(secret, func(t *testing.T) {
			cfg := AuthConfig{
				JWTSecret:       []byte(secret),
				GoogleAudiences: []string{"web.apps.googleusercontent.com"},
			}

			if err := cfg.Validate(EnvProd); err == nil {
				t.Fatal("Validate(prod) = nil for a public example secret")
			}
		})
	}
}
