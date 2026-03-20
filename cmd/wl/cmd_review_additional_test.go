package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
)

func TestDiffBase_UsesUpstreamWhenAvailable(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  remote)
    printf 'upstream\thttps://example/upstream (fetch)\n'
    ;;
  fetch)
    exit 0
    ;;
  *)
    exit 1
    ;;
esac
`)

	if got := diffBase(t.TempDir(), "dolt"); got != "upstream/main" {
		t.Fatalf("diffBase() = %q", got)
	}
}

func TestDiffBase_FallsBackToMainWhenFetchFails(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  remote)
    printf 'upstream\thttps://example/upstream (fetch)\n'
    ;;
  fetch)
    exit 1
    ;;
  *)
    exit 1
    ;;
esac
`)

	if got := diffBase(t.TempDir(), "dolt"); got != "main" {
		t.Fatalf("diffBase() = %q", got)
	}
}

func TestListReviewBranchesRemote(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})
	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			branchesFunc: func(string) ([]string, error) {
				return []string{"wl/alice/w-1", "wl/alice/w-2"}, nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := listReviewBranchesRemote(&stdout, commandTestConfig(t)); err != nil {
		t.Fatalf("listReviewBranchesRemote() error = %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Review branches:") || !strings.Contains(out, "wl/alice/w-2") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestListReviewBranches_Local(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
query=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-q" ]; then
    query="$2"
    shift 2
    continue
  fi
  shift
done
case "$query" in
  *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/%'"*)
    printf 'name\nwl/alice/w-1\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	var stdout bytes.Buffer
	if err := listReviewBranches(&stdout, t.TempDir()); err != nil {
		t.Fatalf("listReviewBranches() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "wl/alice/w-1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestListReviewBranches_LocalEmptyAndError(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		installFakeDolt(t, `#!/bin/sh
set -eu
printf 'name\n'
`)

		var stdout bytes.Buffer
		if err := listReviewBranches(&stdout, t.TempDir()); err != nil {
			t.Fatalf("listReviewBranches() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "No review branches found.") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("error", func(t *testing.T) {
		installFakeDolt(t, "#!/bin/sh\nexit 1\n")

		err := listReviewBranches(io.Discard, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "listing branches") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestShowDiffAndRenderMarkdownDiff(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
args="$*"
case "$args" in
  "diff --stat main...wl/alice/w-1")
    printf 'wanted.csv | 2 +-\n'
    ;;
  "diff -r json main...wl/alice/w-1")
    printf '{"tables":[{"name":"wanted"}]}\n'
    ;;
  "diff -r sql main...wl/alice/w-1")
    printf 'UPDATE wanted SET status = '\''claimed'\'';\n'
    ;;
  "diff main...wl/alice/w-1")
    printf 'full diff\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	for _, tc := range []struct {
		name                    string
		jsonOut, mdOut, statOut bool
		want                    string
	}{
		{name: "stat", statOut: true, want: "wanted.csv | 2 +-"},
		{name: "json", jsonOut: true, want: `"tables"`},
		{name: "default", want: "full diff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := showDiff(&stdout, t.TempDir(), "dolt", "wl/alice/w-1", "main", tc.jsonOut, tc.mdOut, tc.statOut); err != nil {
				t.Fatalf("showDiff() error = %v", err)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}

	var md bytes.Buffer
	if err := renderMarkdownDiff(&md, t.TempDir(), "dolt", "wl/alice/w-1", "main"); err != nil {
		t.Fatalf("renderMarkdownDiff() error = %v", err)
	}
	out := md.String()
	for _, want := range []string{"## wl review: wl/alice/w-1", "### Summary", "### Changes", "UPDATE wanted SET status"} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown output missing %q in %q", want, out)
		}
	}
}

func TestWantedTitleFromBranch(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
printf 'title\nFix auth\n'
`)
	if got := wantedTitleFromBranch("dolt", t.TempDir(), "wl/alice/w-1"); got != "Fix auth" {
		t.Fatalf("wantedTitleFromBranch() = %q", got)
	}
}

func TestWantedTitleFromBranch_FallsBackToBranch(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
exit 1
`)
	branch := "wl/alice/w-1"
	if got := wantedTitleFromBranch("dolt", t.TempDir(), branch); got != branch {
		t.Fatalf("wantedTitleFromBranch() = %q", got)
	}
}

func TestCreatePRForBranchRemote_ProviderAndTokenValidation(t *testing.T) {
	cfg := &federation.Config{
		Upstream:     "hop/wl-commons",
		ProviderType: "github",
	}
	if _, err := createPRForBranchRemote(cfg, scriptedDB{}, "wl/alice/w-1"); err == nil || !strings.Contains(err.Error(), "GitHub PRs require local dolt") {
		t.Fatalf("err = %v", err)
	}

	cfg.ProviderType = "dolthub"
	t.Setenv("DOLTHUB_TOKEN", "")
	if _, err := createPRForBranchRemote(cfg, scriptedDB{}, "wl/alice/w-1"); err == nil || !strings.Contains(err.Error(), "DOLTHUB_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestListPendingItemsFromPRs_UnsupportedProviderReturnsNil(t *testing.T) {
	cfg := &federation.Config{ProviderType: "file"}
	if cb := listPendingItemsFromPRs(cfg); cb != nil {
		t.Fatal("callback should be nil for unsupported providers")
	}
}

func commandTestConfig(t *testing.T) *federation.Config {
	t.Helper()
	store := federation.NewConfigStore()
	cfg, err := store.Load("hop/wl-commons")
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	return cfg
}

func TestShowDiff_MarkdownBranch(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
args="$*"
case "$args" in
  "diff --stat main...wl/alice/w-2")
    printf 'no stat\n'
    ;;
  "diff -r sql main...wl/alice/w-2")
    printf 'DELETE FROM wanted;\n'
    ;;
  *)
    exit 1
    ;;
esac
`)
	var stdout bytes.Buffer
	if err := showDiff(&stdout, t.TempDir(), "dolt", "wl/alice/w-2", "main", false, true, false); err != nil {
		t.Fatalf("showDiff() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "DELETE FROM wanted;") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestListReviewBranchesRemote_Empty(t *testing.T) {
	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			branchesFunc: func(string) ([]string, error) { return nil, nil },
		}, nil
	})
	var stdout bytes.Buffer
	if err := listReviewBranchesRemote(&stdout, &federation.Config{Backend: federation.BackendRemote}); err != nil {
		t.Fatalf("listReviewBranchesRemote() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "No review branches found.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestCloseGitHubPR_Warnings(t *testing.T) {
	var stdout bytes.Buffer
	client := &fakeGitHubPRClient{
		prs:        map[string]fakePR{"alice:wl/alice/w-1": {URL: "https://example/pr/1", Number: "1"}},
		ClosePRErr: io.EOF,
	}
	closeGitHubPR(client, "org/repo", "alice", "fork", "wl/alice/w-1", &stdout)
	if !strings.Contains(stdout.String(), "failed to close PR") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
