package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/dolthubauth"
)

type fakeDolthubAuthStore struct {
	applyErr    error
	checkErr    error
	applyCalled bool
	closeCalled bool
}

func (f *fakeDolthubAuthStore) ApplySchema(context.Context) error {
	f.applyCalled = true
	return f.applyErr
}

func (f *fakeDolthubAuthStore) Check(context.Context) error {
	return f.checkErr
}

func (f *fakeDolthubAuthStore) Close() {
	f.closeCalled = true
}

func (f *fakeDolthubAuthStore) CreateConnectToken(context.Context, []byte, []byte, string, dolthubauth.UserMetadata, time.Time, time.Time) error {
	return nil
}

func (f *fakeDolthubAuthStore) RedeemConnectToken(context.Context, dolthubauth.RedeemInput) (*dolthubauth.Connection, error) {
	return nil, dolthubauth.ErrNotFound
}

func (f *fakeDolthubAuthStore) GetConnection(context.Context, string, string, string, string) (*dolthubauth.Connection, error) {
	return nil, dolthubauth.ErrNotFound
}

func (f *fakeDolthubAuthStore) GetConnectionCredential(context.Context, string, string, string, string, dolthubauth.CredentialCipher) (*dolthubauth.Connection, string, error) {
	return nil, "", dolthubauth.ErrNotFound
}

func (f *fakeDolthubAuthStore) PatchRigHandle(context.Context, string, string, string, string, string, int, time.Time) (*dolthubauth.Connection, error) {
	return nil, dolthubauth.ErrNotFound
}

func (f *fakeDolthubAuthStore) UpsertWasteland(context.Context, string, string, string, string, dolthubauth.WastelandConfig, int, time.Time) (*dolthubauth.Connection, error) {
	return nil, dolthubauth.ErrNotFound
}

func (f *fakeDolthubAuthStore) DeleteWasteland(context.Context, string, string, string, string, string, int, time.Time) (*dolthubauth.Connection, error) {
	return nil, dolthubauth.ErrNotFound
}

func (f *fakeDolthubAuthStore) PatchWastelandSettings(context.Context, string, string, string, string, string, string, bool, int, time.Time) (*dolthubauth.Connection, error) {
	return nil, dolthubauth.ErrNotFound
}

func (f *fakeDolthubAuthStore) UseServiceNonce(context.Context, string, string, time.Time, time.Time) error {
	return nil
}

type fakeDolthubAuthChecker struct {
	checkErr   error
	encryptErr error
	decryptErr error
}

func (f fakeDolthubAuthChecker) Check(context.Context) error {
	return f.checkErr
}

func (f fakeDolthubAuthChecker) Encrypt(context.Context, []byte) ([]byte, string, string, error) {
	return []byte("cipher"), "local-master-key", "local-master-key", f.encryptErr
}

func (f fakeDolthubAuthChecker) Decrypt(context.Context, []byte, string, string) ([]byte, error) {
	return []byte("plain"), f.decryptErr
}

func withServeAuthOverrides(
	t *testing.T,
	loadCfg func() (dolthubauth.Config, error),
	openStore func(context.Context, dolthubauth.Config) (dolthubauth.SchemaStore, error),
	newKeyManager func(dolthubauth.Config) (dolthubauth.ReadinessChecker, error),
) {
	t.Helper()
	oldLoadCfg := loadDolthubAuthConfig
	oldOpenStore := openDolthubAuthStore
	oldNewKeyManager := newDolthubAuthKeyManager
	oldNewServer := newDolthubAuthServer
	if loadCfg != nil {
		loadDolthubAuthConfig = loadCfg
	}
	if openStore != nil {
		openDolthubAuthStore = openStore
	}
	if newKeyManager != nil {
		newDolthubAuthKeyManager = newKeyManager
	}
	t.Cleanup(func() {
		loadDolthubAuthConfig = oldLoadCfg
		openDolthubAuthStore = oldOpenStore
		newDolthubAuthKeyManager = oldNewKeyManager
		newDolthubAuthServer = oldNewServer
	})
}

func TestNewServeCmd_AddsAuthSubcommand(t *testing.T) {
	cmd := newServeCmd(io.Discard, io.Discard)
	auth, _, err := cmd.Find([]string{"auth"})
	if err != nil {
		t.Fatalf("Find(auth) error = %v", err)
	}
	if auth == nil || auth.Name() != "auth" {
		t.Fatalf("auth subcommand = %#v", auth)
	}
}

func TestRunServeAuth_Success(t *testing.T) {
	cfg := dolthubauth.Config{
		ListenAddr:          "127.0.0.1:9101",
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
	store := &fakeDolthubAuthStore{}

	withServeAuthOverrides(
		t,
		func() (dolthubauth.Config, error) { return cfg, nil },
		func(context.Context, dolthubauth.Config) (dolthubauth.SchemaStore, error) { return store, nil },
		func(dolthubauth.Config) (dolthubauth.ReadinessChecker, error) { return fakeDolthubAuthChecker{}, nil },
	)

	var gotAddr string
	withServeListenOverride(t, func(srv *http.Server) error {
		gotAddr = srv.Addr
		return nil
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"serve", "auth"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(serve auth) = %d, stderr = %q", code, stderr.String())
	}
	if !store.applyCalled {
		t.Fatal("ApplySchema was not called")
	}
	if !store.closeCalled {
		t.Fatal("Close was not called")
	}
	if gotAddr != cfg.ListenAddr {
		t.Fatalf("addr = %q, want %q", gotAddr, cfg.ListenAddr)
	}
}

func TestRunServeAuth_ListenAddrFlagOverridesEnvConfig(t *testing.T) {
	cfg := dolthubauth.Config{
		ListenAddr:          "127.0.0.1:9101",
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
	withServeAuthOverrides(
		t,
		func() (dolthubauth.Config, error) { return cfg, nil },
		func(context.Context, dolthubauth.Config) (dolthubauth.SchemaStore, error) {
			return &fakeDolthubAuthStore{}, nil
		},
		func(dolthubauth.Config) (dolthubauth.ReadinessChecker, error) { return fakeDolthubAuthChecker{}, nil },
	)

	var gotAddr string
	withServeListenOverride(t, func(srv *http.Server) error {
		gotAddr = srv.Addr
		return nil
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"serve", "auth", "--listen-addr", "127.0.0.1:9200"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(serve auth --listen-addr) = %d, stderr = %q", code, stderr.String())
	}
	if gotAddr != "127.0.0.1:9200" {
		t.Fatalf("addr = %q", gotAddr)
	}
}

func TestRunServeAuth_ErrorPaths(t *testing.T) {
	t.Run("config load failure", func(t *testing.T) {
		withServeAuthOverrides(
			t,
			func() (dolthubauth.Config, error) { return dolthubauth.Config{}, errors.New("missing env") },
			nil,
			nil,
		)

		var stdout, stderr bytes.Buffer
		if code := run([]string{"serve", "auth"}, &stdout, &stderr); code != 1 {
			t.Fatalf("run(serve auth) = %d", code)
		}
		if !strings.Contains(stderr.String(), "missing env") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})

	t.Run("schema bootstrap failure", func(t *testing.T) {
		cfg := dolthubauth.Config{
			ListenAddr:          "127.0.0.1:9101",
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
		withServeAuthOverrides(
			t,
			func() (dolthubauth.Config, error) { return cfg, nil },
			func(context.Context, dolthubauth.Config) (dolthubauth.SchemaStore, error) {
				return &fakeDolthubAuthStore{applyErr: errors.New("ddl failed")}, nil
			},
			func(dolthubauth.Config) (dolthubauth.ReadinessChecker, error) { return fakeDolthubAuthChecker{}, nil },
		)

		var stdout, stderr bytes.Buffer
		if code := run([]string{"serve", "auth"}, &stdout, &stderr); code != 1 {
			t.Fatalf("run(serve auth) = %d", code)
		}
		if !strings.Contains(stderr.String(), "bootstrap auth-service schema: ddl failed") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})
}
