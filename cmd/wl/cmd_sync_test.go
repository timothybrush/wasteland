package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/federation"
)

func TestRunSync_RemoteModeSkipsLocalDolt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})

	var stdout, stderr bytes.Buffer
	if err := runSync(commandWithWasteland("hop/wl-commons"), &stdout, &stderr, false); err != nil {
		t.Fatalf("runSync() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Remote mode: reads are always fresh") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSync_DryRunShowsChanges(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("FAKE_DOLT_DIFF_MODE", "changes")
	localDir := t.TempDir()
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  fetch)
    exit 0
    ;;
  diff)
    printf 'wanted.csv | 2 +-\n'
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
`)
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  localDir,
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})

	var stdout, stderr bytes.Buffer
	if err := runSync(commandWithWasteland("hop/wl-commons"), &stdout, &stderr, true); err != nil {
		t.Fatalf("runSync() error = %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "wanted.csv | 2 +-") {
		t.Fatalf("stdout = %q", out)
	}
	if strings.Contains(out, "Already up to date") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRunSync_DryRunSurfacesDiffFailures(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	localDir := t.TempDir()
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  fetch)
    exit 0
    ;;
  diff)
    echo 'upstream/main not found' >&2
    exit 2
    ;;
  *)
    exit 0
    ;;
esac
`)
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  localDir,
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})

	var stdout, stderr bytes.Buffer
	err := runSync(commandWithWasteland("hop/wl-commons"), &stdout, &stderr, true)
	if err == nil {
		t.Fatal("runSync() expected error")
	}
	if !strings.Contains(err.Error(), "checking upstream diff") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stdout.String(), "Already up to date") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSync_SuccessUpdatesSyncTimestamp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	localDir := t.TempDir()
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  pull)
    exit 0
    ;;
  sql)
    printf 'open_wanted,total_wanted,total_completions,total_stamps\n1,2,3,4\n'
    ;;
  *)
    exit 0
    ;;
esac
`)
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  localDir,
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})

	var stdout, stderr bytes.Buffer
	if err := runSync(commandWithWasteland("hop/wl-commons"), &stdout, &stderr, false); err != nil {
		t.Fatalf("runSync() error = %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"Synced with upstream", "Open wanted:       1", "Total completions: 3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in %q", want, out)
		}
	}

	store := federation.NewConfigStore()
	cfg, err := store.Load("hop/wl-commons")
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if cfg.LastSyncAt == nil {
		t.Fatal("LastSyncAt = nil, want timestamp")
	}
}
