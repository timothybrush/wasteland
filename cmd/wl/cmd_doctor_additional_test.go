package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/federation"
)

func TestCheckDolt_SuccessAndVersionFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		path := installFakeDolt(t, "#!/bin/sh\nprintf 'dolt version 1.2.3\\n'\n")
		var stdout bytes.Buffer
		diag := checkDolt(&stdout, &doctorDeps{
			lookPath: func(string) (string, error) { return path, nil },
		})
		if diag.status != "pass" || !strings.Contains(diag.message, "1.2.3") {
			t.Fatalf("diag = %+v", diag)
		}
	})

	t.Run("version failure", func(t *testing.T) {
		path := installFakeDolt(t, "#!/bin/sh\nexit 1\n")
		var stdout bytes.Buffer
		diag := checkDolt(&stdout, &doctorDeps{
			lookPath: func(string) (string, error) { return path, nil },
		})
		if diag.status != "warn" || !strings.Contains(diag.message, "found but 'dolt version' failed") {
			t.Fatalf("diag = %+v", diag)
		}
	})
}

func TestCheckDoltCreds_SuccessAndNoKeys(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		credsDir := filepath.Join(home, ".dolt", "creds")
		if err := os.MkdirAll(credsDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(credsDir, "alice.jwk"), []byte("key"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		var stdout bytes.Buffer
		diag := checkDoltCreds(&stdout)
		if diag.status != "pass" || !strings.Contains(diag.message, "1 key(s) found") {
			t.Fatalf("diag = %+v", diag)
		}
	})

	t.Run("no keys", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		credsDir := filepath.Join(home, ".dolt", "creds")
		if err := os.MkdirAll(credsDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(credsDir, "README.txt"), []byte("no keys"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		var stdout bytes.Buffer
		diag := checkDoltCreds(&stdout)
		if diag.status != "warn" || !strings.Contains(diag.message, "no key files found") {
			t.Fatalf("diag = %+v", diag)
		}
	})
}

func TestCheckDoltCreds_MissingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout bytes.Buffer
	diag := checkDoltCreds(&stdout)
	if diag.status != "warn" || !strings.Contains(diag.message, "no credentials directory found") {
		t.Fatalf("diag = %+v", diag)
	}
	if !strings.Contains(diag.fixHint, "dolt login") {
		t.Fatalf("diag = %+v", diag)
	}
}

func TestCheckWastelands_ListErrorAndRemoteMode(t *testing.T) {
	t.Run("list error", func(t *testing.T) {
		var stdout bytes.Buffer
		results := checkWastelands(&stdout, &doctorDeps{
			store: &fakeConfigStore{listErr: errors.New("boom")},
		})
		if len(results) != 1 || results[0].status != "fail" || !strings.Contains(results[0].message, "boom") {
			t.Fatalf("results = %+v", results)
		}
	})

	t.Run("remote mode", func(t *testing.T) {
		var stdout bytes.Buffer
		results := checkWastelands(&stdout, &doctorDeps{
			getenv: func(key string) string {
				if key == "DOLTHUB_TOKEN" {
					return "token"
				}
				return ""
			},
			store: &fakeConfigStore{configs: map[string]*federation.Config{
				"hop/wl-commons": {
					Upstream: "hop/wl-commons",
					Backend:  federation.BackendRemote,
					Mode:     federation.ModePR,
				},
			}},
		})
		if len(results) < 2 {
			t.Fatalf("results = %+v", results)
		}
		if !strings.Contains(stdout.String(), "Backend: remote") || !strings.Contains(stdout.String(), "DOLTHUB_TOKEN: set") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("remote mode missing token", func(t *testing.T) {
		var stdout bytes.Buffer
		results := checkWastelands(&stdout, &doctorDeps{
			getenv: func(string) string { return "" },
			store: &fakeConfigStore{configs: map[string]*federation.Config{
				"hop/wl-commons": {
					Upstream: "hop/wl-commons",
					Backend:  federation.BackendRemote,
					Mode:     federation.ModePR,
				},
			}},
		})
		if len(results) < 2 || results[1].status != "fail" {
			t.Fatalf("results = %+v", results)
		}
		if !strings.Contains(stdout.String(), "required for remote mode") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("local clone present", func(t *testing.T) {
		localDir := t.TempDir()
		var stdout bytes.Buffer
		results := checkWastelands(&stdout, &doctorDeps{
			lookPath: func(string) (string, error) { return "", &notFoundErr{} },
			store: &fakeConfigStore{configs: map[string]*federation.Config{
				"hop/wl-commons": {
					Upstream: "hop/wl-commons",
					LocalDir: localDir,
					Backend:  federation.BackendLocal,
					Signing:  false,
				},
			}},
		})
		var clonePass bool
		for _, d := range results {
			if d.name == "hop/wl-commons/clone" && d.status == "pass" {
				clonePass = true
			}
		}
		if !clonePass || !strings.Contains(stdout.String(), localDir) {
			t.Fatalf("results = %+v stdout=%q", results, stdout.String())
		}
	})
}

func TestCheckGPGSigning_SuccessAndMissingKeyConfig(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		doltPath := installFakeDolt(t, `#!/bin/sh
set -eu
case "$*" in
  "config --global --get sqlserver.global.signingkey")
    printf 'ABC123\n'
    ;;
  *)
    exit 1
    ;;
esac
`)
		installFakeCommand(t, "gpg", "#!/bin/sh\nexit 0\n")

		var stdout bytes.Buffer
		diag := checkGPGSigning(&stdout, &federation.Config{Signing: true}, &doctorDeps{
			lookPath: func(string) (string, error) { return doltPath, nil },
		})
		if diag.status != "pass" || !strings.Contains(diag.message, "ABC123") {
			t.Fatalf("diag = %+v", diag)
		}
	})

	t.Run("missing configured key", func(t *testing.T) {
		doltPath := installFakeDolt(t, "#!/bin/sh\nexit 1\n")

		var stdout bytes.Buffer
		diag := checkGPGSigning(&stdout, &federation.Config{Signing: true}, &doctorDeps{
			lookPath: func(string) (string, error) { return doltPath, nil },
		})
		if diag.status != "fail" || !strings.Contains(diag.message, "no signing key configured") {
			t.Fatalf("diag = %+v", diag)
		}
	})
}

func TestRunDoctor_CheckFlag_AllPass_RemoteHealthy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credsDir := filepath.Join(home, ".dolt", "creds")
	if err := os.MkdirAll(credsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(credsDir, "alice.jwk"), []byte("key"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	doltPath := installFakeDolt(t, "#!/bin/sh\nprintf 'dolt version 1.2.3\\n'\n")

	var stdout bytes.Buffer
	err := runDoctor(
		&stdout,
		io.Discard,
		func(string) (string, error) { return doltPath, nil },
		func(key string) string {
			switch key {
			case "DOLTHUB_TOKEN":
				return "token"
			case "DOLTHUB_ORG":
				return "alice"
			default:
				return ""
			}
		},
		&fakeConfigStore{configs: map[string]*federation.Config{
			"hop/wl-commons": {
				Upstream: "hop/wl-commons",
				Backend:  federation.BackendRemote,
				Mode:     federation.ModePR,
			},
		}},
		false,
		true,
	)
	if err != nil {
		t.Fatalf("runDoctor() error = %v\nstdout=%s", err, stdout.String())
	}
}

func TestRunDoctor_FixFlag_RepairsOrphanedClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credsDir := filepath.Join(home, ".dolt", "creds")
	if err := os.MkdirAll(credsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(credsDir, "alice.jwk"), []byte("key"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	doltPath := installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  version)
    printf 'dolt version 1.2.3\n'
    ;;
  clone)
    mkdir -p "$3"
    ;;
  *)
    exit 1
    ;;
esac
`)

	localDir := filepath.Join(t.TempDir(), "missing-clone")
	var stdout bytes.Buffer
	err := runDoctor(
		&stdout,
		io.Discard,
		func(string) (string, error) { return doltPath, nil },
		func(string) string { return "" },
		&fakeConfigStore{configs: map[string]*federation.Config{
			"hop/wl-commons": {
				Upstream:    "hop/wl-commons",
				LocalDir:    localDir,
				Backend:     federation.BackendLocal,
				UpstreamURL: "https://example/hop/wl-commons",
				Signing:     false,
			},
		}},
		true,
		false,
	)
	if err != nil {
		t.Fatalf("runDoctor() error = %v", err)
	}
	if _, statErr := os.Stat(localDir); statErr != nil {
		t.Fatalf("clone dir was not created: %v", statErr)
	}
	if !strings.Contains(stdout.String(), "Fixing hop/wl-commons/clone") || !strings.Contains(stdout.String(), "fixed") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestCheckStaleSync_FixFuncPullsAndUpdatesTimestamp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	oldSync := time.Now().Add(-48 * time.Hour)
	cfg := &federation.Config{
		Upstream:   "hop/wl-commons",
		LocalDir:   t.TempDir(),
		LastSyncAt: &oldSync,
	}
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$*" in
  "pull upstream main")
    exit 0
    ;;
  *)
    exit 1
    ;;
esac
`)

	var stdout bytes.Buffer
	diags := checkStaleSync(&stdout, cfg, "hop/wl-commons")
	if len(diags) != 1 || diags[0].fixFunc == nil {
		t.Fatalf("diags = %+v", diags)
	}
	if err := diags[0].fixFunc(); err != nil {
		t.Fatalf("fixFunc() error = %v", err)
	}
	if cfg.LastSyncAt == nil || time.Since(*cfg.LastSyncAt) > time.Minute {
		t.Fatalf("LastSyncAt was not refreshed: %+v", cfg.LastSyncAt)
	}
}
