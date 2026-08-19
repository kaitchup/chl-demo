package config

import (
	"os"
	"strconv"
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

	// Per-merchant webhook secrets (comma-separated for rotation).
	YokiWebhookKeys         string
	SudyWebhookKeys         string
	ShirlyWebhookKeys       string
	TiaWebhookKeys          string
	CassielWebhookKeys      string
	TestMerchantWebhookKeys string
	ProdDummy1WebhookKeys   string

	// Sandbox / simulate-settlement module (Dolos admin account).
	// When DolosEmail is empty the sandbox endpoints return 503.
	DolosEmail           string
	DolosPassword        string
	AdminBaseURL         string
	MerchantAdminBaseURL string
	SandboxMaxAMLRetries int
}

// SandboxEnabled reports whether the sandbox simulate module is configured.
func (c Config) SandboxEnabled() bool {
	return c.DolosEmail != "" && c.AdminBaseURL != ""
}

// UpayEnabled reports whether real UPay integration is configured.
func (c Config) UpayEnabled() bool {
	return c.UpayAPIKey != "" && c.UpayBaseURL != ""
}

// WebhookSecrets returns the UPay signing secrets (new first for rotation).
func (c Config) WebhookSecrets() []string {
	return splitSecrets(c.UpayWebhookKeys)
}

// YokiWebhookSecrets returns the Yoki signing secrets (new first for rotation).
func (c Config) YokiWebhookSecrets() []string {
	return splitSecrets(c.YokiWebhookKeys)
}

// SudyWebhookSecrets returns the Sudy signing secrets (new first for rotation).
func (c Config) SudyWebhookSecrets() []string {
	return splitSecrets(c.SudyWebhookKeys)
}

// ShirlyWebhookSecrets returns the Shirly signing secrets (new first for rotation).
func (c Config) ShirlyWebhookSecrets() []string {
	return splitSecrets(c.ShirlyWebhookKeys)
}

// TiaWebhookSecrets returns the Tia signing secrets (new first for rotation).
func (c Config) TiaWebhookSecrets() []string {
	return splitSecrets(c.TiaWebhookKeys)
}

// CassielWebhookSecrets returns the Cassiel signing secrets (new first for rotation).
func (c Config) CassielWebhookSecrets() []string {
	return splitSecrets(c.CassielWebhookKeys)
}

// TestMerchantWebhookSecrets returns the mch_test_merchant signing secrets (new first for rotation).
func (c Config) TestMerchantWebhookSecrets() []string {
	return splitSecrets(c.TestMerchantWebhookKeys)
}

// ProdDummy1WebhookSecrets returns the prod_dummy1 signing secrets (new first for rotation).
func (c Config) ProdDummy1WebhookSecrets() []string {
	return splitSecrets(c.ProdDummy1WebhookKeys)
}

func splitSecrets(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ",") {
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
		DatabaseURL:             env("DATABASE_URL", "postgres://chl:chl@localhost:5432/chl?sslmode=disable"),
		JWTSecret:               env("JWT_SECRET", "dev-secret-change-me"),
		JWTTTL:                  ttl,
		PublicBaseURL:           env("PUBLIC_BASE_URL", "http://localhost"),
		UpayBaseURL:             env("UPAY_BASE_URL", ""),
		UpayAPIKey:              env("UPAY_API_KEY", ""),
		UpayWebhookKeys:         env("UPAY_WEBHOOK_SECRET", ""),
		UpayCACertPath:          env("UPAY_CA_CERT", ""),
		ReconcileInterval:       reconcile,
		YokiWebhookKeys:         env("YOKI_WEBHOOK_SECRET", ""),
		SudyWebhookKeys:         env("SUDY_WEBHOOK_SECRET", ""),
		ShirlyWebhookKeys:       env("SHIRLY_WEBHOOK_SECRET", ""),
		TiaWebhookKeys:          env("TIA_WEBHOOK_SECRET", ""),
		CassielWebhookKeys:      env("CASSIEL_WEBHOOK_SECRET", ""),
		TestMerchantWebhookKeys: env("TEST_MERCHANT_WEBHOOK_SECRET", ""),
		ProdDummy1WebhookKeys:   env("PROD_DUMMY1_WEBHOOK_SECRET", ""),
		DolosEmail:              env("DOLOS_EMAIL", ""),
		DolosPassword:           env("DOLOS_PASSWORD", ""),
		AdminBaseURL:            env("ADMIN_BASE_URL", ""),
		MerchantAdminBaseURL:    env("MERCHANT_ADMIN_BASE_URL", ""),
		SandboxMaxAMLRetries:    envInt("SANDBOX_MAX_AML_RETRIES", 3),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
