package federation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeFederationDolt(t *testing.T, body string) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dolt"), []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake dolt: %v", err)
	}

	logPath := filepath.Join(root, "dolt.log")
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOLT_LOG", logPath)
	return root, logPath
}

func readFederationLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestExecDoltCLI_SuccessPaths(t *testing.T) {
	root, logPath := writeFakeFederationDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "clone" ]; then
  mkdir -p "$3/.dolt"
  exit 0
fi
if [ "$1" = "remote" ] && [ "$2" = "-v" ]; then
  printf 'origin https://example.com/repo (fetch)\n'
  exit 0
fi
exit 0
`)

	cli := &execDoltCLI{}
	cloneDir := filepath.Join(root, "clone", "commons")
	if err := cli.Clone("https://remote.example/repo", cloneDir); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	// Existing clone should no-op.
	if err := cli.Clone("https://remote.example/repo", cloneDir); err != nil {
		t.Fatalf("Clone(existing) error = %v", err)
	}

	initDir := filepath.Join(root, "worktree")
	if err := cli.Init(initDir); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := cli.SQLExec(initDir, "SELECT 1"); err != nil {
		t.Fatalf("SQLExec() error = %v", err)
	}
	if err := cli.StageAndCommit(initDir, "Initial schema", true); err != nil {
		t.Fatalf("StageAndCommit() error = %v", err)
	}
	if err := cli.AddRemote(initDir, "origin", "https://remote.example/repo"); err != nil {
		t.Fatalf("AddRemote() error = %v", err)
	}
	if err := cli.RegisterRig(initDir, "alice", "alice-org", "Alice", "alice@example.com", "v1", true); err != nil {
		t.Fatalf("RegisterRig() error = %v", err)
	}
	if err := cli.Push(initDir); err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if err := cli.PushBranch(initDir, "wl/register/alice", true); err != nil {
		t.Fatalf("PushBranch(force) error = %v", err)
	}
	if err := cli.PushBranch(initDir, "wl/register/alice", false); err != nil {
		t.Fatalf("PushBranch() error = %v", err)
	}
	if err := cli.CheckoutBranch(initDir, "wl/register/alice"); err != nil {
		t.Fatalf("CheckoutBranch() error = %v", err)
	}
	if err := cli.CheckoutMain(initDir); err != nil {
		t.Fatalf("CheckoutMain() error = %v", err)
	}
	if err := cli.AddUpstreamRemote(initDir, "https://remote.example/upstream"); err != nil {
		t.Fatalf("AddUpstreamRemote() error = %v", err)
	}

	logText := readFederationLog(t, logPath)
	for _, want := range []string{
		"clone https://remote.example/repo",
		"init",
		"sql -q SELECT 1",
		"add -A",
		"commit -S -m Initial schema",
		"remote add origin https://remote.example/repo",
		"commit -S -m Register rig: alice",
		"push origin main",
		"push --force origin wl/register/alice",
		"push origin wl/register/alice",
		"branch wl/register/alice",
		"checkout wl/register/alice",
		"checkout main",
		"remote add upstream https://remote.example/upstream",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("command log missing %q in %q", want, logText)
		}
	}
}

func TestExecDoltCLI_GracefulNoopPaths(t *testing.T) {
	_, logPath := writeFakeFederationDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "commit" ]; then
  echo "nothing to commit" >&2
  exit 1
fi
if [ "$1" = "remote" ] && [ "$2" = "-v" ]; then
  printf 'upstream https://remote.example/upstream (fetch)\n'
  exit 0
fi
if [ "$1" = "remote" ] && [ "$2" = "add" ]; then
  echo "already exists" >&2
  exit 1
fi
if [ "$1" = "branch" ]; then
  echo "already exists" >&2
  exit 1
fi
exit 0
`)

	cli := &execDoltCLI{}
	workDir := t.TempDir()

	if err := cli.StageAndCommit(workDir, "noop", false); err != nil {
		t.Fatalf("StageAndCommit(noop) error = %v", err)
	}
	if err := cli.AddRemote(workDir, "origin", "https://remote.example/repo"); err != nil {
		t.Fatalf("AddRemote(already-exists) error = %v", err)
	}
	if err := cli.CheckoutBranch(workDir, "wl/register/alice"); err != nil {
		t.Fatalf("CheckoutBranch(existing) error = %v", err)
	}
	if err := cli.AddUpstreamRemote(workDir, "https://remote.example/upstream"); err != nil {
		t.Fatalf("AddUpstreamRemote(existing) error = %v", err)
	}

	logText := readFederationLog(t, logPath)
	if strings.Contains(logText, "remote add upstream") {
		t.Fatalf("did not expect upstream add when remote already exists: %q", logText)
	}
}

func TestExecDoltCLI_RegisterRigSigningErrorAndConstructors(t *testing.T) {
	_, _ = writeFakeFederationDolt(t, `#!/bin/sh
if [ "$1" = "commit" ]; then
  echo "invalid user id" >&2
  exit 1
fi
exit 0
`)

	cli := &execDoltCLI{}
	workDir := t.TempDir()
	err := cli.RegisterRig(workDir, "alice", "alice-org", "Alice", "alice@example.com", "v1", true)
	if err == nil || !strings.Contains(err.Error(), "GPG signing failed") {
		t.Fatalf("RegisterRig() error = %v, want GPG guidance", err)
	}

	provider := NewFakeProvider()
	svc := NewService(provider)
	if svc.Remote != provider || svc.CLI == nil || svc.Config == nil {
		t.Fatalf("NewService() = %+v, want provider + real deps", svc)
	}

	store := NewFakeConfigStore()
	svc = NewServiceWith(provider, store)
	if svc.Remote != provider || svc.Config != store || svc.CLI == nil {
		t.Fatalf("NewServiceWith() = %+v, want explicit store", svc)
	}
}
