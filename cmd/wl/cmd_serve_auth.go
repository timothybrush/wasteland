package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gastownhall/wasteland/internal/api"
	"github.com/gastownhall/wasteland/internal/dolthubauth"
	"github.com/gastownhall/wasteland/internal/observability"
	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"
)

var (
	loadDolthubAuthConfig = dolthubauth.LoadConfigFromEnv
	openDolthubAuthStore  = func(ctx context.Context, cfg dolthubauth.Config) (dolthubauth.SchemaStore, error) {
		return dolthubauth.OpenPostgres(ctx, cfg.DatabaseURL, cfg.TenantID, cfg.Environment)
	}
	newDolthubAuthKeyManager = dolthubauth.NewCredentialCipher
	newDolthubAuthServer     = dolthubauth.NewServer
)

func newServeAuthCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Start the standalone DoltHub auth service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServeAuth(cmd, stdout, stderr)
		},
	}
	cmd.Flags().String("listen-addr", "", "Listen address override for the auth service")
	return cmd
}

func runServeAuth(cmd *cobra.Command, stdout, _ io.Writer) error {
	cfg, err := loadDolthubAuthConfig()
	if err != nil {
		return err
	}
	if listenAddr, _ := cmd.Flags().GetString("listen-addr"); listenAddr != "" {
		cfg.ListenAddr = listenAddr
		if err := cfg.Validate(); err != nil {
			return err
		}
	}

	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	slog.SetDefault(logger)

	shutdownTelemetry, telemetryEnabled, err := observability.Init(context.Background(), observability.Config{
		ServiceName:      "dolthub-auth",
		ServiceNamespace: "wasteland",
		ServiceVersion:   version,
		Environment:      cfg.Environment,
	})
	if err != nil {
		slog.Warn("otel init failed", "error", err)
	} else if telemetryEnabled {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if shutdownErr := shutdownTelemetry(shutdownCtx); shutdownErr != nil {
				slog.Warn("otel shutdown failed", "error", shutdownErr)
			}
		}()
	}

	initSentry(cfg.Environment)
	defer sentry.Flush(2 * time.Second)

	startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := openDolthubAuthStore(startupCtx, cfg)
	if err != nil {
		return fmt.Errorf("open auth-service store: %w", err)
	}
	defer store.Close()

	if err := store.ApplySchema(startupCtx); err != nil {
		return fmt.Errorf("bootstrap auth-service schema: %w", err)
	}

	keyManager, err := newDolthubAuthKeyManager(startupCtx, cfg)
	if err != nil {
		return fmt.Errorf("configure auth-service key manager: %w", err)
	}
	if closer, ok := keyManager.(io.Closer); ok {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				slog.Warn("auth-service key manager close failed", "error", closeErr)
			}
		}()
	}
	authStore, ok := store.(dolthubauth.AuthStore)
	if !ok {
		return fmt.Errorf("auth-service store does not implement runtime auth operations")
	}

	server, err := newDolthubAuthServer(cfg, dolthubauth.Dependencies{
		Store:      authStore,
		KeyManager: keyManager,
		Version:    version,
	})
	if err != nil {
		return fmt.Errorf("build auth-service server: %w", err)
	}

	handler := observability.NewHTTPHandler(api.RequestLog(logger)(api.SecurityHeaders(server.Handler())))
	slog.Info("server started", "mode", "dolthub-auth", "addr", cfg.ListenAddr, "tenant_id", cfg.TenantID, "environment", cfg.Environment)
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: handler, MaxHeaderBytes: 1 << 20} //nolint:gosec // bind addr is configured by operator input
	return serveListen(srv)
}
