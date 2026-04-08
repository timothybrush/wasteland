package dolthubauth

import (
	"os"
	"strings"
	"testing"
)

func TestLoadConfigFromEnv_Success(t *testing.T) {
	t.Setenv("DOLTHUB_AUTH_LISTEN_ADDR", "127.0.0.1:9100")
	t.Setenv("DOLTHUB_AUTH_DATABASE_URL", "postgres://auth:secret@localhost/auth")
	t.Setenv("DOLTHUB_AUTH_TENANT_ID", "tenant-dev")
	t.Setenv("DOLTHUB_AUTH_ENVIRONMENT", "dev")
	t.Setenv("DOLTHUB_AUTH_CURRENT_KEY_ID", "current-key")
	t.Setenv("DOLTHUB_AUTH_CURRENT_SHARED_SECRET", "current-secret")
	t.Setenv("DOLTHUB_AUTH_NEXT_KEY_ID", "next-key")
	t.Setenv("DOLTHUB_AUTH_NEXT_SHARED_SECRET", "next-secret")
	t.Setenv("DOLTHUB_AUTH_TOKEN_PEPPER", "token-pepper")
	t.Setenv("DOLTHUB_AUTH_REDEEM_PEPPER", "redeem-pepper")
	t.Setenv("DOLTHUB_AUTH_MASTER_KEY", "local-master-key")
	t.Setenv("DOLTHUB_AUTH_ALLOWED_ORIGINS", "https://app.example, https://staging.example ")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9100" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[1] != "https://staging.example" {
		t.Fatalf("AllowedOrigins = %#v", cfg.AllowedOrigins)
	}
}

func TestLoadConfigFromEnv_MissingRequired(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("DOLTHUB_AUTH_ALLOWED_ORIGINS", "https://app.example")

	_, err := LoadConfigFromEnv()
	if err == nil || !strings.Contains(err.Error(), "DOLTHUB_AUTH_LISTEN_ADDR is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigValidate_RequiresNextKeyPair(t *testing.T) {
	cfg := validConfig()
	cfg.NextKeyID = "next-key"
	cfg.NextSharedSecret = ""

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigValidate_RejectsProductionLocalMasterKey(t *testing.T) {
	cfg := validConfig()
	cfg.Environment = "production"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "DOLTHUB_AUTH_ALLOW_LOCAL_MASTER_KEY_IN_PRODUCTION=true") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigValidate_AllowsProductionLocalMasterKeyWithExplicitOverride(t *testing.T) {
	cfg := validConfig()
	cfg.Environment = "production"
	cfg.AllowLocalMasterKeyInProduction = true

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func clearAuthEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DOLTHUB_AUTH_LISTEN_ADDR",
		"DOLTHUB_AUTH_DATABASE_URL",
		"DOLTHUB_AUTH_TENANT_ID",
		"DOLTHUB_AUTH_ENVIRONMENT",
		"DOLTHUB_AUTH_CURRENT_KEY_ID",
		"DOLTHUB_AUTH_CURRENT_SHARED_SECRET",
		"DOLTHUB_AUTH_NEXT_KEY_ID",
		"DOLTHUB_AUTH_NEXT_SHARED_SECRET",
		"DOLTHUB_AUTH_TOKEN_PEPPER",
		"DOLTHUB_AUTH_REDEEM_PEPPER",
		"DOLTHUB_AUTH_MASTER_KEY",
		"DOLTHUB_AUTH_ALLOWED_ORIGINS",
	} {
		t.Setenv(key, "")
	}
}

func validConfig() Config {
	return Config{
		ListenAddr:          "127.0.0.1:9100",
		DatabaseURL:         "postgres://auth:secret@localhost/auth",
		TenantID:            "tenant-dev",
		Environment:         "dev",
		CurrentKeyID:        "current-key",
		CurrentSharedSecret: "current-secret",
		TokenPepper:         "token-pepper",
		RedeemPepper:        "redeem-pepper",
		MasterKey:           "local-master-key",
		AllowedOrigins:      []string{"https://app.example"},
	}
}

func TestSplitAndTrimCSV(t *testing.T) {
	got := splitAndTrimCSV(" https://a.example , ,https://b.example ")
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("splitAndTrimCSV() = %#v", got)
	}
}

func TestIsProductionEnvironment(t *testing.T) {
	for _, value := range []string{"prod", "production", " PROD "} {
		if !isProductionEnvironment(value) {
			t.Fatalf("isProductionEnvironment(%q) = false", value)
		}
	}
	if isProductionEnvironment("staging") {
		t.Fatal("staging unexpectedly treated as production")
	}
}

func TestLoadConfigFromEnv_DoesNotDependOnAmbientProcessEnv(t *testing.T) {
	clearAuthEnv(t)
	for _, pair := range [][2]string{
		{"DOLTHUB_AUTH_LISTEN_ADDR", "127.0.0.1:9100"},
		{"DOLTHUB_AUTH_DATABASE_URL", "postgres://auth:secret@localhost/auth"},
		{"DOLTHUB_AUTH_TENANT_ID", "tenant-dev"},
		{"DOLTHUB_AUTH_ENVIRONMENT", "dev"},
		{"DOLTHUB_AUTH_CURRENT_KEY_ID", "current-key"},
		{"DOLTHUB_AUTH_CURRENT_SHARED_SECRET", "current-secret"},
		{"DOLTHUB_AUTH_TOKEN_PEPPER", "token-pepper"},
		{"DOLTHUB_AUTH_REDEEM_PEPPER", "redeem-pepper"},
		{"DOLTHUB_AUTH_MASTER_KEY", "local-master-key"},
		{"DOLTHUB_AUTH_ALLOWED_ORIGINS", "https://app.example"},
	} {
		if err := os.Setenv(pair[0], pair[1]); err != nil {
			t.Fatalf("Setenv(%q) error = %v", pair[0], err)
		}
	}
	t.Cleanup(func() { clearAuthEnv(t) })

	if _, err := LoadConfigFromEnv(); err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
}

func TestParseBoolEnv(t *testing.T) {
	for _, value := range []string{"true", " TRUE ", "1", "yes", "on"} {
		if !parseBoolEnv(value) {
			t.Fatalf("parseBoolEnv(%q) = false", value)
		}
	}
	for _, value := range []string{"", "false", "0", "no", "off"} {
		if parseBoolEnv(value) {
			t.Fatalf("parseBoolEnv(%q) = true", value)
		}
	}
}
