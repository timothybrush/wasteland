package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
)

func TestCreateRequiresArg(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	root := newRootCmd(&stdout, &stderr)

	for _, c := range root.Commands() {
		if c.Name() == "create" {
			if err := c.Args(c, []string{}); err == nil {
				t.Error("create should require exactly 1 argument")
			}
			if err := c.Args(c, []string{"org/db"}); err != nil {
				t.Errorf("create should accept 1 argument: %v", err)
			}
			if err := c.Args(c, []string{"a", "b"}); err == nil {
				t.Error("create should reject 2 arguments")
			}
			return
		}
	}
	t.Fatal("create command not found")
}

func TestCreateInvalidUpstream(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := runCreate(&stdout, &stderr, "noslash", "", "", "", "",
		"", "", false, "", true, false)
	if err == nil {
		t.Fatal("expected error for invalid upstream")
	}
}

func TestCreateAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	// Create the fake .dolt directory at the expected path.
	doltDir := filepath.Join(dir, "wasteland", "org", "db", ".dolt")
	if err := os.MkdirAll(doltDir, 0o755); err != nil {
		t.Fatalf("creating fake .dolt dir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runCreate(&stdout, &stderr, "org/db", "", "", "", "",
		"", "", false, "", true, false)
	if err == nil {
		t.Fatal("expected error when database already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want to contain 'already exists'", err.Error())
	}
}

func TestRunCreate_AdditionalProviderModes(t *testing.T) {
	tests := []struct {
		name             string
		upstream         string
		githubLocal      string
		localOnly        bool
		env              map[string]string
		wantProviderType string
		wantPushed       bool
	}{
		{
			name:             "local only uses dolthub stub provider",
			upstream:         "org/wl-local",
			localOnly:        true,
			wantProviderType: "dolthub",
			wantPushed:       false,
		},
		{
			name:             "github local provider",
			upstream:         "org/wl-github",
			githubLocal:      "/tmp/github",
			wantProviderType: "github",
			wantPushed:       true,
		},
		{
			name:             "default dolthub provider",
			upstream:         "org/wl-commons",
			env:              map[string]string{"DOLTHUB_TOKEN": "token", "DOLTHUB_ORG": "org"},
			wantProviderType: "dolthub",
			wantPushed:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			installFakeDolt(t, "#!/bin/sh\nexit 0\n")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			var gotProviderType string
			withCreateWithProviderOverride(t, func(_ io.Writer, provider remote.Provider, _ federation.ConfigStore, opts federation.CreateOptions) (*federation.CreateResult, error) {
				gotProviderType = provider.Type()
				return &federation.CreateResult{Config: &federation.Config{
					Upstream:  opts.Upstream,
					LocalDir:  "/tmp/" + filepath.Base(opts.Upstream),
					RigHandle: "alice",
				}}, nil
			})

			var stdout bytes.Buffer
			err := runCreate(&stdout, io.Discard, tc.upstream, "", "alice", "Alice", "alice@example.com", "", "", false, tc.githubLocal, tc.localOnly, false)
			if err != nil {
				t.Fatalf("runCreate() error = %v", err)
			}
			if gotProviderType != tc.wantProviderType {
				t.Fatalf("provider type = %q", gotProviderType)
			}
			out := stdout.String()
			hasPushed := strings.Contains(out, "Pushed to")
			if hasPushed != tc.wantPushed {
				t.Fatalf("output = %q", out)
			}
		})
	}
}

func TestRunCreate_ServiceErrorReturnsErrExit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	installFakeDolt(t, "#!/bin/sh\nexit 0\n")
	withCreateWithProviderOverride(t, func(io.Writer, remote.Provider, federation.ConfigStore, federation.CreateOptions) (*federation.CreateResult, error) {
		return nil, errExit
	})

	var stdout bytes.Buffer
	if err := runCreate(&stdout, io.Discard, "org/wl-commons", "", "alice", "Alice", "alice@example.com", "", "", false, "", true, false); !errors.Is(err, errExit) {
		t.Fatalf("err = %v", err)
	}
}
