package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeExecutable(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestFileProvider_NoPRSupportAndType(t *testing.T) {
	t.Parallel()

	p := NewFileProvider("/tmp/remotes")
	if got := p.DatabaseURL("org", "db"); got != "file:///tmp/remotes/org/db" {
		t.Fatalf("DatabaseURL() = %q, want file:///tmp/remotes/org/db", got)
	}
	url, err := p.CreatePR("fork-org", "upstream-org", "db", "branch", "title", "body")
	if err != nil {
		t.Fatalf("CreatePR() error = %v, want nil", err)
	}
	if url != "" {
		t.Fatalf("CreatePR() URL = %q, want empty", url)
	}
	if got := p.Type(); got != "file" {
		t.Fatalf("Type() = %q, want file", got)
	}
}

func TestGitProvider_NoPRSupportAndType(t *testing.T) {
	t.Parallel()

	p := NewGitProvider("/tmp/remotes")
	url, err := p.CreatePR("fork-org", "upstream-org", "db", "branch", "title", "body")
	if err != nil {
		t.Fatalf("CreatePR() error = %v, want nil", err)
	}
	if url != "" {
		t.Fatalf("CreatePR() URL = %q, want empty", url)
	}
	if got := p.Type(); got != "git" {
		t.Fatalf("Type() = %q, want git", got)
	}
}

func TestFileProvider_ForkRunsCloneRemoteAddAndPush(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "dolt.log")
	writeFakeExecutable(t, dir, "dolt", `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "clone" ]; then
  mkdir -p "$3"
fi
exit 0
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOLT_LOG", logPath)

	p := NewFileProvider(filepath.Join(dir, "remotes"))
	if err := p.Fork("src-org", "testdb", "fork-org"); err != nil {
		t.Fatalf("Fork() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "remotes", "fork-org", "testdb")); err != nil {
		t.Fatalf("expected fork destination to exist: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading dolt log: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"clone file://" + filepath.Join(dir, "remotes", "src-org", "testdb"),
		"remote add fork-dest file://" + filepath.Join(dir, "remotes", "fork-org", "testdb"),
		"push fork-dest main",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dolt log missing %q in %q", want, got)
		}
	}
}

func TestGitProvider_ForkSeedsBareRepoAndPushes(t *testing.T) {
	dir := t.TempDir()
	doltLog := filepath.Join(dir, "dolt.log")
	gitLog := filepath.Join(dir, "git.log")
	writeFakeExecutable(t, dir, "dolt", `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "clone" ]; then
  mkdir -p "$3"
fi
exit 0
`)
	writeFakeExecutable(t, dir, "git", `#!/bin/sh
echo "$@" >> "$GIT_LOG"
exit 0
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOLT_LOG", doltLog)
	t.Setenv("GIT_LOG", gitLog)

	p := NewGitProvider(filepath.Join(dir, "remotes"))
	if err := p.Fork("src-org", "testdb", "fork-org"); err != nil {
		t.Fatalf("Fork() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "remotes", "fork-org", "testdb.git")); err != nil {
		t.Fatalf("expected bare git destination to exist: %v", err)
	}

	gitData, err := os.ReadFile(gitLog)
	if err != nil {
		t.Fatalf("reading git log: %v", err)
	}
	gitLogText := string(gitData)
	for _, want := range []string{
		"init --bare " + filepath.Join(dir, "remotes", "fork-org", "testdb.git"),
		"init -b main",
		"commit --allow-empty -m init",
		"push file://" + filepath.Join(dir, "remotes", "fork-org", "testdb.git") + " main",
	} {
		if !strings.Contains(gitLogText, want) {
			t.Fatalf("git log missing %q in %q", want, gitLogText)
		}
	}
}

func TestSeedBareGitRepo_InvokesInitCommitAndPush(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "git.log")
	writeFakeExecutable(t, dir, "git", `#!/bin/sh
echo "$@" >> "$GIT_LOG"
exit 0
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_LOG", logPath)

	bareDir := filepath.Join(dir, "repo.git")
	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		t.Fatalf("creating bare dir: %v", err)
	}

	if err := seedBareGitRepo(bareDir); err != nil {
		t.Fatalf("seedBareGitRepo() error = %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading git log: %v", err)
	}
	logText := string(data)
	for _, want := range []string{"init -b main", "commit --allow-empty -m init", "push file://" + bareDir + " main"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("git log missing %q in %q", want, logText)
		}
	}
}

func TestGitHubProvider_ForkInvokesGHWithExpectedArguments(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "gh.log")
	writeFakeExecutable(t, dir, "gh", `#!/bin/sh
printf '%s\n' "$@" > "$GH_LOG"
exit 0
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_LOG", logPath)

	p := NewGitHubProvider()
	if err := p.Fork("upstream-org", "wl-commons", "fork-org"); err != nil {
		t.Fatalf("Fork() error = %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading gh log: %v", err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{
		"repo",
		"fork",
		"upstream-org/wl-commons",
		"--org",
		"fork-org",
		"--clone=false",
	}, "\n")
	if got != want {
		t.Fatalf("gh args = %q, want %q", got, want)
	}
}

func TestGitHubProvider_ForkTreatsAlreadyExistsAsSuccess(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "gh", `#!/bin/sh
echo "a fork already exists" >&2
exit 1
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := NewGitHubProvider()
	if err := p.Fork("upstream-org", "wl-commons", "fork-org"); err != nil {
		t.Fatalf("Fork() error = %v, want nil", err)
	}
}

func TestGitHubProvider_CreatePRReturnsURL(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "gh", `#!/bin/sh
echo "https://github.com/upstream-org/wl-commons/pull/42"
exit 0
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := NewGitHubProvider()
	url, err := p.CreatePR("fork-org", "upstream-org", "wl-commons", "wl/alice/w-1", "Title", "Body")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if url != "https://github.com/upstream-org/wl-commons/pull/42" {
		t.Fatalf("CreatePR() URL = %q, want pull URL", url)
	}
}

func TestGitHubProvider_CreatePRReturnsExistingURLOnDuplicate(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "gh", `#!/bin/sh
echo "a pull request already exists for fork-org:wl/alice/w-1" >&2
echo "https://github.com/upstream-org/wl-commons/pull/42" >&2
exit 1
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := NewGitHubProvider()
	url, err := p.CreatePR("fork-org", "upstream-org", "wl-commons", "wl/alice/w-1", "Title", "Body")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if url != "https://github.com/upstream-org/wl-commons/pull/42" {
		t.Fatalf("CreatePR() URL = %q, want existing pull URL", url)
	}
}
