package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/federation"
)

type fakeDoltHubPRProvider struct {
	createPRFn func(string, string, string, string, string, string) (string, error)
	findPRFn   func(string, string, string, string) (string, string)
	updatePRFn func(string, string, string, string, string) error
	closePRFn  func(string, string, string) error
}

func (p fakeDoltHubPRProvider) CreatePR(forkOrg, upstreamOrg, db, fromBranch, title, body string) (string, error) {
	if p.createPRFn != nil {
		return p.createPRFn(forkOrg, upstreamOrg, db, fromBranch, title, body)
	}
	return "", nil
}

func (p fakeDoltHubPRProvider) FindPR(upstreamOrg, db, forkOrg, fromBranch string) (string, string) {
	if p.findPRFn != nil {
		return p.findPRFn(upstreamOrg, db, forkOrg, fromBranch)
	}
	return "", ""
}

func (p fakeDoltHubPRProvider) UpdatePR(upstreamOrg, db, prID, title, description string) error {
	if p.updatePRFn != nil {
		return p.updatePRFn(upstreamOrg, db, prID, title, description)
	}
	return nil
}

func (p fakeDoltHubPRProvider) ClosePR(upstreamOrg, db, prID string) error {
	if p.closePRFn != nil {
		return p.closePRFn(upstreamOrg, db, prID)
	}
	return nil
}

func withPushBranchOverride(t *testing.T, fn func(string, string, string, bool, io.Writer) error) {
	t.Helper()
	old := pushBranchToRemoteForce
	pushBranchToRemoteForce = fn
	t.Cleanup(func() {
		pushBranchToRemoteForce = old
	})
}

func withGitHubPRClientOverride(t *testing.T, fn func(string) GitHubPRClient) {
	t.Helper()
	old := newGitHubPRClientFromPath
	newGitHubPRClientFromPath = fn
	t.Cleanup(func() {
		newGitHubPRClientFromPath = old
	})
}

func withDoltHubPRProviderOverride(t *testing.T, fn func(string) doltHubPRProvider) {
	t.Helper()
	old := newDoltHubPRProvider
	newDoltHubPRProvider = fn
	t.Cleanup(func() {
		newDoltHubPRProvider = old
	})
}

func installReviewDolt(t *testing.T) {
	t.Helper()
	installFakeDolt(t, `#!/bin/sh
set -eu
args="$*"
case "$args" in
  "remote -v")
    printf 'origin\thttps://example/origin (fetch)\n'
    ;;
  "diff --stat main...wl/alice/w-go-1")
    printf 'wanted.csv | 2 +-\n'
    ;;
  "diff -r sql main...wl/alice/w-go-1")
    printf 'UPDATE wanted SET status = '\''claimed'\'';\n'
    ;;
  *)
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
      *"SELECT title FROM wanted AS OF 'wl/alice/w-go-1'"*)
        printf 'title\nFix auth\n'
        ;;
      *)
        exit 1
        ;;
    esac
    ;;
esac
`)
}

func TestRunReview_CreatePRLocalGitHub(t *testing.T) {
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
	installReviewDolt(t)
	installFakeCommand(t, "gh", "#!/bin/sh\nexit 0\n")

	var pushed []string
	withPushBranchOverride(t, func(dbDir, remote, branch string, force bool, _ io.Writer) error {
		pushed = []string{dbDir, remote, branch, fmt.Sprintf("%v", force)}
		return nil
	})
	withGitHubPRClientOverride(t, func(string) GitHubPRClient {
		return &fakeGitHubPRClient{
			GetRefSHA:        "head-sha",
			GetCommitTreeSHA: "tree-sha",
			CreateBlobSHA:    "blob-sha",
			CreateTreeSHA:    "new-tree",
			CreateCommitSHA:  "commit-sha",
			CreatePRURL:      "https://example/pr/9",
		}
	})

	var stdout bytes.Buffer
	cmd := commandWithWasteland("org/repo")
	_ = cmd.Flags().Set("local-db", "true")
	if err := runReview(cmd, &stdout, io.Discard, "wl/alice/w-go-1", false, false, false, true); err != nil {
		t.Fatalf("runReview() error = %v", err)
	}
	if len(pushed) != 4 || pushed[1] != "origin" || pushed[2] != "wl/alice/w-go-1" || pushed[3] != "true" {
		t.Fatalf("pushed = %+v", pushed)
	}
	if out := stdout.String(); !strings.Contains(out, "PR:") || !strings.Contains(out, "https://example/pr/9") {
		t.Fatalf("output = %q", out)
	}
}

func TestCreatePRForBranchGitHub_ReturnsCreatedURL(t *testing.T) {
	installReviewDolt(t)
	installFakeCommand(t, "gh", "#!/bin/sh\nexit 0\n")

	withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error { return nil })
	withGitHubPRClientOverride(t, func(string) GitHubPRClient {
		return &fakeGitHubPRClient{
			GetRefSHA:        "head-sha",
			GetCommitTreeSHA: "tree-sha",
			CreateBlobSHA:    "blob-sha",
			CreateTreeSHA:    "new-tree",
			CreateCommitSHA:  "commit-sha",
			CreatePRURL:      "https://example/pr/10",
		}
	})

	cfg := &federation.Config{
		Upstream:     "org/repo",
		ForkOrg:      "alice",
		ForkDB:       "repo",
		LocalDir:     t.TempDir(),
		ProviderType: "github",
	}
	url, err := createPRForBranchGitHub(cfg, "dolt", "wl/alice/w-go-1", "main")
	if err != nil {
		t.Fatalf("createPRForBranchGitHub() error = %v", err)
	}
	if url != "https://example/pr/10" {
		t.Fatalf("url = %q", url)
	}
}

func TestRunDoltHubPR_AndCreatePRForBranchDoltHub(t *testing.T) {
	t.Setenv("DOLTHUB_TOKEN", "token")
	installReviewDolt(t)

	withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error { return nil })

	t.Run("runDoltHubPR", func(t *testing.T) {
		withDoltHubPRProviderOverride(t, func(token string) doltHubPRProvider {
			if token != "token" {
				t.Fatalf("token = %q", token)
			}
			return fakeDoltHubPRProvider{
				createPRFn: func(_, _, _, branch, title, body string) (string, error) {
					if branch != "wl/alice/w-go-1" || !strings.Contains(title, "Fix auth") || !strings.Contains(body, "UPDATE wanted") {
						t.Fatalf("got %q %q %q", branch, title, body)
					}
					return "https://dolthub.example/pr/1", nil
				},
			}
		})

		cfg := &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
		}
		var stdout bytes.Buffer
		if err := runDoltHubPR(&stdout, cfg, "dolt", "wl/alice/w-go-1", "main"); err != nil {
			t.Fatalf("runDoltHubPR() error = %v", err)
		}
		if out := stdout.String(); !strings.Contains(out, "https://dolthub.example/pr/1") {
			t.Fatalf("output = %q", out)
		}
	})

	t.Run("createPRForBranchDoltHub updates existing", func(t *testing.T) {
		var updated bool
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				createPRFn: func(string, string, string, string, string, string) (string, error) {
					return "", fmt.Errorf("already exists")
				},
				findPRFn: func(string, string, string, string) (string, string) {
					return "https://dolthub.example/pr/existing", "42"
				},
				updatePRFn: func(_, _, prID, title, body string) error {
					updated = prID == "42" && strings.Contains(title, "Fix auth") && strings.Contains(body, "UPDATE wanted")
					return nil
				},
			}
		})

		cfg := &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
		}
		url, err := createPRForBranchDoltHub(cfg, "dolt", "wl/alice/w-go-1", "main")
		if err != nil {
			t.Fatalf("createPRForBranchDoltHub() error = %v", err)
		}
		if url != "https://dolthub.example/pr/existing" || !updated {
			t.Fatalf("url = %q updated = %v", url, updated)
		}
	})
}

func TestCreatePRForBranchRemote_DoltHubLifecycle(t *testing.T) {
	t.Setenv("DOLTHUB_TOKEN", "token")

	cfg := &federation.Config{
		Upstream:     "hop/wl-commons",
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		ProviderType: "dolthub",
	}
	db := scriptedDB{
		queryFunc: func(query, ref string) (string, error) {
			if ref != "wl/alice/w-1" {
				t.Fatalf("ref = %q", ref)
			}
			if !strings.Contains(query, "WHERE w.id='w-1'") {
				t.Fatalf("query = %q", query)
			}
			return "id,title,description,project,type,priority,tags,posted_by,claimed_by,status,effort_level,created_at,updated_at,completion_id,completion_wanted_id,completed_by,evidence,completion_stamp_id,validated_by,stamp_record_id,stamp_author,stamp_subject,stamp_valence,stamp_severity,stamp_context_id,stamp_context_type,stamp_skill_tags,stamp_message\nw-1,Fix auth,,,,2,,alice,,claimed,medium,,,,,,,,,,,,,,,\n", nil
		},
	}

	t.Run("create", func(t *testing.T) {
		withDoltHubPRProviderOverride(t, func(token string) doltHubPRProvider {
			if token != "token" {
				t.Fatalf("token = %q", token)
			}
			return fakeDoltHubPRProvider{
				createPRFn: func(forkOrg, upstreamOrg, db, fromBranch, title, body string) (string, error) {
					if forkOrg != "alice" || upstreamOrg != "hop" || db != "wl-commons" {
						t.Fatalf("got %q %q %q", forkOrg, upstreamOrg, db)
					}
					if fromBranch != "wl/alice/w-1" || title != "[wl] Fix auth" || body != "" {
						t.Fatalf("got %q %q %q", fromBranch, title, body)
					}
					return "https://dolthub.example/pr/44", nil
				},
			}
		})

		url, err := createPRForBranchRemote(cfg, db, "wl/alice/w-1")
		if err != nil {
			t.Fatalf("createPRForBranchRemote() error = %v", err)
		}
		if url != "https://dolthub.example/pr/44" {
			t.Fatalf("url = %q", url)
		}
	})

	t.Run("update existing", func(t *testing.T) {
		var updated bool
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				createPRFn: func(string, string, string, string, string, string) (string, error) {
					return "", fmt.Errorf("already exists")
				},
				findPRFn: func(upstreamOrg, db, forkOrg, fromBranch string) (string, string) {
					if upstreamOrg != "hop" || db != "wl-commons" || forkOrg != "alice" || fromBranch != "wl/alice/w-1" {
						t.Fatalf("got %q %q %q %q", upstreamOrg, db, forkOrg, fromBranch)
					}
					return "https://dolthub.example/pr/existing", "42"
				},
				updatePRFn: func(upstreamOrg, db, prID, title, body string) error {
					updated = upstreamOrg == "hop" &&
						db == "wl-commons" &&
						prID == "42" &&
						title == "[wl] Fix auth" &&
						body == ""
					return nil
				},
			}
		})

		url, err := createPRForBranchRemote(cfg, db, "wl/alice/w-1")
		if err != nil {
			t.Fatalf("createPRForBranchRemote() error = %v", err)
		}
		if url != "https://dolthub.example/pr/existing" || !updated {
			t.Fatalf("url = %q updated = %v", url, updated)
		}
	})

	t.Run("fallback to pulls page", func(t *testing.T) {
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				createPRFn: func(string, string, string, string, string, string) (string, error) {
					return "", fmt.Errorf("already exists")
				},
				findPRFn: func(string, string, string, string) (string, string) {
					return "", ""
				},
			}
		})

		url, err := createPRForBranchRemote(cfg, db, "wl/alice/w-1")
		if err != nil {
			t.Fatalf("createPRForBranchRemote() error = %v", err)
		}
		if url != "https://www.dolthub.com/repositories/hop/wl-commons/pulls" {
			t.Fatalf("url = %q", url)
		}
	})
}

func TestRunDoltHubPR_ErrorAndExistingPRPaths(t *testing.T) {
	installReviewDolt(t)

	cfg := &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  t.TempDir(),
		RigHandle: "alice",
	}

	t.Run("missing token", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "")

		err := runDoltHubPR(io.Discard, cfg, "dolt", "wl/alice/w-go-1", "main")
		if err == nil || !strings.Contains(err.Error(), "DOLTHUB_TOKEN") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("push failure", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "token")
		withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error {
			return fmt.Errorf("push failed")
		})

		err := runDoltHubPR(io.Discard, cfg, "dolt", "wl/alice/w-go-1", "main")
		if err == nil || !strings.Contains(err.Error(), "pushing to DoltHub fork") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("update existing warning", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "token")
		withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error { return nil })
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				createPRFn: func(string, string, string, string, string, string) (string, error) {
					return "", fmt.Errorf("already exists")
				},
				findPRFn: func(string, string, string, string) (string, string) {
					return "https://dolthub.example/pr/existing", "42"
				},
				updatePRFn: func(string, string, string, string, string) error {
					return io.EOF
				},
			}
		})

		var stdout bytes.Buffer
		if err := runDoltHubPR(&stdout, cfg, "dolt", "wl/alice/w-go-1", "main"); err != nil {
			t.Fatalf("runDoltHubPR() error = %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "warning: could not update existing PR") || !strings.Contains(out, "https://dolthub.example/pr/existing") {
			t.Fatalf("stdout = %q", out)
		}
	})

	t.Run("fallback pulls page", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "token")
		withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error { return nil })
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				createPRFn: func(string, string, string, string, string, string) (string, error) {
					return "", fmt.Errorf("already exists")
				},
				findPRFn: func(string, string, string, string) (string, string) {
					return "", ""
				},
			}
		})

		var stdout bytes.Buffer
		if err := runDoltHubPR(&stdout, cfg, "dolt", "wl/alice/w-go-1", "main"); err != nil {
			t.Fatalf("runDoltHubPR() error = %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "PR already exists for this branch.") || !strings.Contains(out, "https://www.dolthub.com/repositories/hop/wl-commons/pulls") {
			t.Fatalf("stdout = %q", out)
		}
	})

	t.Run("create error", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "token")
		withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error { return nil })
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				createPRFn: func(string, string, string, string, string, string) (string, error) {
					return "", fmt.Errorf("boom")
				},
			}
		})

		err := runDoltHubPR(io.Discard, cfg, "dolt", "wl/alice/w-go-1", "main")
		if err == nil || !strings.Contains(err.Error(), "creating DoltHub PR: boom") {
			t.Fatalf("err = %v", err)
		}
	})
}
