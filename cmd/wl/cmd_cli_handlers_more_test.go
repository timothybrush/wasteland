package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestRunLeaderboard_RemoteAndLocalSync(t *testing.T) {
	t.Run("remote", func(t *testing.T) {
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
			return scriptedDB{}, nil
		})
		withLeaderboardOverride(t, func(_ commons.DB, limit int) ([]commons.LeaderboardEntry, error) {
			if limit != 10 {
				t.Fatalf("limit = %d", limit)
			}
			return []commons.LeaderboardEntry{{
				RigHandle:   "alice",
				Completions: 7,
				AvgQuality:  4.5,
				AvgReliab:   4.0,
				TopSkills:   []string{"go", "auth"},
			}}, nil
		})

		var stdout bytes.Buffer
		if err := runLeaderboard(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, 10); err != nil {
			t.Fatalf("runLeaderboard(remote) error = %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "Leaderboard (1 rigs):") || !strings.Contains(out, "alice") || !strings.Contains(out, "go, auth") {
			t.Fatalf("output = %q", out)
		}
	})

	t.Run("local sync failure", func(t *testing.T) {
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
		installFakeDolt(t, "#!/bin/sh\nexit 0\n")
		withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
			return scriptedDB{
				syncFunc: func() error { return fmt.Errorf("network down") },
			}, nil
		})

		cmd := commandWithWasteland("hop/wl-commons")
		_ = cmd.Flags().Set("local-db", "true")
		err := runLeaderboard(cmd, io.Discard, io.Discard, 5)
		if err == nil || !strings.Contains(err.Error(), "syncing with upstream: network down") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRunVerify_UsesConfiguredDatabaseDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	localDir := t.TempDir()
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  localDir,
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	pwdFile := filepath.Join(t.TempDir(), "pwd.txt")
	installFakeDolt(t, fmt.Sprintf(`#!/bin/sh
set -eu
printf '%%s' "$*" > %q
printf '%%s' "$PWD" > %q
printf 'signature ok\n'
`, argsFile, pwdFile))

	var stdout bytes.Buffer
	if err := runVerify(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, 7); err != nil {
		t.Fatalf("runVerify() error = %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "signature ok") {
		t.Fatalf("stdout = %q", got)
	}
	if got, err := os.ReadFile(argsFile); err != nil || string(got) != "log --show-signature -n 7" {
		t.Fatalf("args = %q, err = %v", string(got), err)
	}
	if got, err := os.ReadFile(pwdFile); err != nil || string(got) != localDir {
		t.Fatalf("pwd = %q, err = %v", string(got), err)
	}
}

func TestRunVerify_ReportsCommandFailure(t *testing.T) {
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
	installFakeDolt(t, "#!/bin/sh\nexit 1\n")

	err := runVerify(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, 3)
	if err == nil || !strings.Contains(err.Error(), "dolt log --show-signature") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunApproveAndRequestChanges_GitHubFlow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:     "org/repo",
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		RigHandle:    "alice",
		ProviderType: "github",
		Backend:      federation.BackendRemote,
		JoinedAt:     time.Now(),
	})

	endpointFile := filepath.Join(t.TempDir(), "endpoint.txt")
	bodyFile := filepath.Join(t.TempDir(), "body.txt")
	installFakeCommand(t, "gh", fmt.Sprintf(`#!/bin/sh
set -eu
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  printf '[{"number":7,"url":"https://example/pr/7"}]'
  exit 0
fi
if [ "$1" = "api" ]; then
  printf '%%s' "$2" > %q
  cat > %q
  printf '{}'
  exit 0
fi
exit 1
`, endpointFile, bodyFile))

	var approveOut bytes.Buffer
	if err := runApprove(commandWithWasteland("org/repo"), &approveOut, io.Discard, "wl/alice/w-1", "LGTM"); err != nil {
		t.Fatalf("runApprove() error = %v", err)
	}
	if out := approveOut.String(); !strings.Contains(out, "Approved wl/alice/w-1") || !strings.Contains(out, "https://example/pr/7") {
		t.Fatalf("approve output = %q", out)
	}
	if body, err := os.ReadFile(bodyFile); err != nil || !strings.Contains(string(body), `"event":"APPROVE"`) {
		t.Fatalf("approve body = %q, err = %v", string(body), err)
	}

	var requestOut bytes.Buffer
	if err := runRequestChanges(commandWithWasteland("org/repo"), &requestOut, io.Discard, "wl/alice/w-1", "needs tests"); err != nil {
		t.Fatalf("runRequestChanges() error = %v", err)
	}
	if out := requestOut.String(); !strings.Contains(out, "Requested changes on wl/alice/w-1") {
		t.Fatalf("request output = %q", out)
	}
	if endpoint, err := os.ReadFile(endpointFile); err != nil || string(endpoint) != "repos/org/repo/pulls/7/reviews" {
		t.Fatalf("endpoint = %q, err = %v", string(endpoint), err)
	}
	if body, err := os.ReadFile(bodyFile); err != nil || !strings.Contains(string(body), `"event":"REQUEST_CHANGES"`) {
		t.Fatalf("request body = %q, err = %v", string(body), err)
	}
}

func TestRunInferStatus_UsesResolvedWantedID(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) {
		if id != "short" {
			t.Fatalf("id = %q", id)
		}
		return "w-123", nil
	})
	withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
		return fakeCommandClient{
			detailFn: func(wantedID string) (*sdk.DetailResult, error) {
				if wantedID != "w-123" {
					t.Fatalf("wantedID = %q", wantedID)
				}
				return &sdk.DetailResult{
					Item: &commons.WantedItem{
						ID:          "w-123",
						Title:       "Run inference",
						Status:      "completed",
						Type:        "inference",
						Description: `{"prompt":"Summarize diff","model":"gpt-5","seed":11}`,
					},
				}, nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runInferStatus(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "short"); err != nil {
		t.Fatalf("runInferStatus() error = %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "w-123: Run inference") || !strings.Contains(out, "Inference Job:") {
		t.Fatalf("output = %q", out)
	}
}

func TestRunMergeRemote_MergesAndDeletesBranch(t *testing.T) {
	var merged, deleted string
	cfg := &federation.Config{
		Upstream: "hop/wl-commons",
		Backend:  federation.BackendRemote,
	}
	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			mergeBranchFunc: func(branch string) error {
				merged = branch
				return nil
			},
			deleteBranchFunc: func(branch string) error {
				deleted = branch
				return nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runMergeRemote(&stdout, cfg, "wl/alice/w-1", false); err != nil {
		t.Fatalf("runMergeRemote() error = %v", err)
	}
	if merged != "wl/alice/w-1" || deleted != "wl/alice/w-1" {
		t.Fatalf("merged = %q deleted = %q", merged, deleted)
	}
	if out := stdout.String(); !strings.Contains(out, "Merged wl/alice/w-1 into main") || !strings.Contains(out, "Branch wl/alice/w-1 deleted") {
		t.Fatalf("output = %q", out)
	}
}

func TestRunMergeRemote_ErrorAndKeepBranchPaths(t *testing.T) {
	cfg := &federation.Config{
		Upstream: "hop/wl-commons",
		Backend:  federation.BackendRemote,
	}

	t.Run("open db error", func(t *testing.T) {
		withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
			return nil, fmt.Errorf("boom")
		})
		if err := runMergeRemote(io.Discard, cfg, "wl/alice/w-1", false); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("merge error", func(t *testing.T) {
		withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
			return scriptedDB{
				mergeBranchFunc: func(string) error { return fmt.Errorf("merge failed") },
			}, nil
		})
		if err := runMergeRemote(io.Discard, cfg, "wl/alice/w-1", false); err == nil || !strings.Contains(err.Error(), "merging branch: merge failed") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("delete warning", func(t *testing.T) {
		withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
			return scriptedDB{
				mergeBranchFunc:  func(string) error { return nil },
				deleteBranchFunc: func(string) error { return fmt.Errorf("delete failed") },
			}, nil
		})

		var stdout bytes.Buffer
		if err := runMergeRemote(&stdout, cfg, "wl/alice/w-1", false); err != nil {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(stdout.String(), "warning: failed to delete branch") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("keep branch", func(t *testing.T) {
		var deleted bool
		withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
			return scriptedDB{
				mergeBranchFunc:  func(string) error { return nil },
				deleteBranchFunc: func(string) error { deleted = true; return nil },
			}, nil
		})

		if err := runMergeRemote(io.Discard, cfg, "wl/alice/w-1", true); err != nil {
			t.Fatalf("err = %v", err)
		}
		if deleted {
			t.Fatal("branch delete should be skipped when keepBranch is true")
		}
	})
}
