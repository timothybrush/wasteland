package dolthubauth

import (
	"fmt"
	"os"
	"strings"
)

// Config is the environment-backed runtime configuration for the standalone
// DoltHub auth service.
type Config struct {
	ListenAddr                      string
	DatabaseURL                     string
	TenantID                        string
	Environment                     string
	CurrentKeyID                    string
	CurrentSharedSecret             string
	NextKeyID                       string
	NextSharedSecret                string
	TokenPepper                     string
	RedeemPepper                    string
	MasterKey                       string
	AllowLocalMasterKeyInProduction bool
	AllowedOrigins                  []string
}

// LoadConfigFromEnv reads the auth-service runtime contract from environment
// variables and validates the result.
func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		ListenAddr:                      strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_LISTEN_ADDR")),
		DatabaseURL:                     strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_DATABASE_URL")),
		TenantID:                        strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_TENANT_ID")),
		Environment:                     strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_ENVIRONMENT")),
		CurrentKeyID:                    strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_CURRENT_KEY_ID")),
		CurrentSharedSecret:             strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_CURRENT_SHARED_SECRET")),
		NextKeyID:                       strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_NEXT_KEY_ID")),
		NextSharedSecret:                strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_NEXT_SHARED_SECRET")),
		TokenPepper:                     strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_TOKEN_PEPPER")),
		RedeemPepper:                    strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_REDEEM_PEPPER")),
		MasterKey:                       strings.TrimSpace(os.Getenv("DOLTHUB_AUTH_MASTER_KEY")),
		AllowLocalMasterKeyInProduction: parseBoolEnv(os.Getenv("DOLTHUB_AUTH_ALLOW_LOCAL_MASTER_KEY_IN_PRODUCTION")),
		AllowedOrigins:                  splitAndTrimCSV(os.Getenv("DOLTHUB_AUTH_ALLOWED_ORIGINS")),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks the current skeleton's required runtime contract.
func (cfg Config) Validate() error {
	required := []struct {
		key   string
		value string
	}{
		{key: "DOLTHUB_AUTH_LISTEN_ADDR", value: cfg.ListenAddr},
		{key: "DOLTHUB_AUTH_DATABASE_URL", value: cfg.DatabaseURL},
		{key: "DOLTHUB_AUTH_TENANT_ID", value: cfg.TenantID},
		{key: "DOLTHUB_AUTH_ENVIRONMENT", value: cfg.Environment},
		{key: "DOLTHUB_AUTH_CURRENT_KEY_ID", value: cfg.CurrentKeyID},
		{key: "DOLTHUB_AUTH_CURRENT_SHARED_SECRET", value: cfg.CurrentSharedSecret},
		{key: "DOLTHUB_AUTH_TOKEN_PEPPER", value: cfg.TokenPepper},
		{key: "DOLTHUB_AUTH_REDEEM_PEPPER", value: cfg.RedeemPepper},
		{key: "DOLTHUB_AUTH_MASTER_KEY", value: cfg.MasterKey},
	}
	for _, requiredValue := range required {
		value := strings.TrimSpace(requiredValue.value)
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", requiredValue.key)
		}
	}
	if len(cfg.AllowedOrigins) == 0 {
		return fmt.Errorf("DOLTHUB_AUTH_ALLOWED_ORIGINS is required")
	}
	if (cfg.NextKeyID == "") != (cfg.NextSharedSecret == "") {
		return fmt.Errorf("DOLTHUB_AUTH_NEXT_KEY_ID and DOLTHUB_AUTH_NEXT_SHARED_SECRET must be set together")
	}
	if isProductionEnvironment(cfg.Environment) && !cfg.AllowLocalMasterKeyInProduction {
		return fmt.Errorf("production auth-service startup requires a KMS-backed encryption backend or DOLTHUB_AUTH_ALLOW_LOCAL_MASTER_KEY_IN_PRODUCTION=true")
	}
	return nil
}

func splitAndTrimCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func isProductionEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "prod", "production":
		return true
	default:
		return false
	}
}

func parseBoolEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}
