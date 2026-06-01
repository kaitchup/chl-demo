package config

import (
	"os"
	"strings"
	"time"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	DatabaseURL   string
	JWTSecret     string
	JWTTTL        time.Duration
	PublicBaseURL string

	// UPay integration (v0.0.2). When UpayAPIKey is empty the app runs in
	// placeholder mode (no real payment), so local dev works without NetBird.
	UpayBaseURL       string
	UpayAPIKey        string
	UpayWebhookKeys   string // comma-separated to support key rotation
	UpayCACertPath    string
	ReconcileInterval time.Duration
}

// UpayEnabled reports whether real UPay integration is configured.
func (c Config) UpayEnabled() bool {
	return c.UpayAPIKey != "" && c.UpayBaseURL != ""
}

// WebhookSecrets returns the configured signing secrets (new first for rotation).
func (c Config) WebhookSecrets() []string {
	var out []string
	for _, s := range strings.Split(c.UpayWebhookKeys, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func Load() Config {
	ttl, err := time.ParseDuration(env("JWT_TTL", "24h"))
	if err != nil {
		ttl = 24 * time.Hour
	}
	reconcile, err := time.ParseDuration(env("RECONCILE_INTERVAL", "60s"))
	if err != nil {
		reconcile = 60 * time.Second
	}
	return Config{
		DatabaseURL:       env("DATABASE_URL", "postgres://chl:chl@localhost:5432/chl?sslmode=disable"),
		JWTSecret:         env("JWT_SECRET", "dev-secret-change-me"),
		JWTTTL:            ttl,
		PublicBaseURL:     env("PUBLIC_BASE_URL", "http://localhost"),
		UpayBaseURL:       env("UPAY_BASE_URL", ""),
		UpayAPIKey:        env("UPAY_API_KEY", ""),
		UpayWebhookKeys:   env("UPAY_WEBHOOK_SECRET", ""),
		UpayCACertPath:    env("UPAY_CA_CERT", ""),
		ReconcileInterval: reconcile,
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
