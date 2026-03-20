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

func TestMergeApprovalWarning(t *testing.T) {
	tests := []struct {
		name                string
		hasApproval         bool
		hasChangesRequested bool
		want                string
	}{
		{
			name:                "changes requested",
			hasChangesRequested: true,
			want:                "PR has outstanding change requests",
		},
		{
			name: "no approvals",
			want: "PR has no approvals",
		},
		{
			name:        "approved",
			hasApproval: true,
			want:        "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeApprovalWarning(tc.hasApproval, tc.hasChangesRequested)
			if got != tc.want {
				t.Errorf("mergeApprovalWarning(%v, %v) = %q, want %q",
					tc.hasApproval, tc.hasChangesRequested, got, tc.want)
			}
		})
	}
}

func TestMergeRequiresArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newRootCmd(&stdout, &stderr)

	for _, c := range root.Commands() {
		if c.Name() == "merge" {
			if err := c.Args(c, []string{}); err == nil {
				t.Error("merge should require exactly 1 argument")
			}
			if err := c.Args(c, []string{"wl/rig/w-abc"}); err != nil {
				t.Errorf("merge should accept 1 argument: %v", err)
			}
			if err := c.Args(c, []string{"a", "b"}); err == nil {
				t.Error("merge should reject 2 arguments")
			}
			return
		}
	}
	t.Fatal("merge command not found")
}

func TestRunMerge_RemoteAndLocalPaths(t *testing.T) {
	t.Run("remote success", func(t *testing.T) {
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
				mergeBranchFunc:  func(string) error { return nil },
				deleteBranchFunc: func(string) error { return nil },
			}, nil
		})

		var stdout bytes.Buffer
		if err := runMerge(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "wl/alice/w-1", false, false); err != nil {
			t.Fatalf("runMerge(remote) error = %v", err)
		}
		if out := stdout.String(); !strings.Contains(out, "Merged wl/alice/w-1 into main") {
			t.Fatalf("output = %q", out)
		}
	})

	t.Run("remote no-push unsupported", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			RigHandle: "alice",
			Backend:   federation.BackendRemote,
			JoinedAt:  time.Now(),
		})
		err := runMerge(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "wl/alice/w-1", true, false)
		if err == nil || !strings.Contains(err.Error(), "--no-push is not supported in remote mode") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("local missing branch", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
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
  *"SELECT COUNT(*) AS cnt FROM dolt_branches WHERE name = 'wl/alice/w-1'"*)
    printf 'cnt\n0\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

		cmd := commandWithWasteland("hop/wl-commons")
		_ = cmd.Flags().Set("local-db", "true")
		err := runMerge(cmd, io.Discard, io.Discard, "wl/alice/w-1", false, false)
		if err == nil || !strings.Contains(err.Error(), `branch "wl/alice/w-1" does not exist`) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("local success", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
		installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  sql)
    if [ "$2" = "-r" ] && [ "$3" = "csv" ] && [ "$4" = "-q" ]; then
      case "$5" in
        *"SELECT COUNT(*) AS cnt FROM dolt_branches WHERE name = 'wl/alice/w-1'"*)
          printf 'cnt\n1\n'
          ;;
        *)
          exit 1
          ;;
      esac
      exit 0
    fi
    if [ "$2" = "--file" ]; then
      exit 0
    fi
    ;;
  checkout)
    exit 0
    ;;
  push)
    exit 0
    ;;
esac
exit 1
`)

		cmd := commandWithWasteland("hop/wl-commons")
		_ = cmd.Flags().Set("local-db", "true")
		var stdout bytes.Buffer
		if err := runMerge(cmd, &stdout, io.Discard, "wl/alice/w-1", false, false); err != nil {
			t.Fatalf("runMerge() error = %v", err)
		}
		if out := stdout.String(); !strings.Contains(out, "Merged wl/alice/w-1 into main") || !strings.Contains(out, "Branch wl/alice/w-1 deleted") {
			t.Fatalf("output = %q", out)
		}
	})

	t.Run("local github closes pr", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:     "org/repo",
			ForkOrg:      "alice",
			ForkDB:       "repo",
			LocalDir:     t.TempDir(),
			RigHandle:    "alice",
			ProviderType: "github",
			Backend:      federation.BackendLocal,
			JoinedAt:     time.Now(),
		})
		installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  sql)
    if [ "$2" = "-r" ] && [ "$3" = "csv" ] && [ "$4" = "-q" ]; then
      case "$5" in
        *"SELECT COUNT(*) AS cnt FROM dolt_branches WHERE name = 'wl/alice/w-1'"*)
          printf 'cnt\n1\n'
          ;;
        *)
          exit 1
          ;;
      esac
      exit 0
    fi
    if [ "$2" = "--file" ]; then
      exit 0
    fi
    ;;
  checkout|push)
    exit 0
    ;;
esac
exit 1
`)
		installFakeCommand(t, "gh", "#!/bin/sh\nexit 0\n")
		withGitHubPRClientOverride(t, func(string) GitHubPRClient {
			return &fakeGitHubPRClient{
				prs: map[string]fakePR{
					"alice:wl/alice/w-1": {URL: "https://example/pr/1", Number: "1"},
				},
				reviews: map[string][]byte{
					"1": []byte(`[{"user":{"login":"reviewer"},"state":"APPROVED"}]`),
				},
			}
		})

		cmd := commandWithWasteland("org/repo")
		_ = cmd.Flags().Set("local-db", "true")
		var stdout bytes.Buffer
		if err := runMerge(cmd, &stdout, io.Discard, "wl/alice/w-1", false, false); err != nil {
			t.Fatalf("runMerge(github) error = %v", err)
		}
		if out := stdout.String(); !strings.Contains(out, "Closed PR https://example/pr/1") {
			t.Fatalf("output = %q", out)
		}
	})
}
