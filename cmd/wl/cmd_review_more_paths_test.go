package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/federation"
)

func TestCreatePRForBranch_MissingDolt(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	cfg := &federation.Config{
		Upstream:     "hop/wl-commons",
		LocalDir:     t.TempDir(),
		ProviderType: "github",
	}
	if _, err := createPRForBranch(cfg, "wl/alice/w-1"); err == nil || !strings.Contains(err.Error(), "dolt not found in PATH") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunGitHubPR_ErrorPaths(t *testing.T) {
	cfg := &federation.Config{
		Upstream:  "org/repo",
		ForkOrg:   "alice",
		ForkDB:    "repo",
		LocalDir:  t.TempDir(),
		RigHandle: "alice",
	}

	t.Run("missing gh", func(t *testing.T) {
		dir := t.TempDir()
		doltPath := filepath.Join(dir, "dolt")
		if err := os.WriteFile(doltPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		t.Setenv("PATH", dir)

		err := runGitHubPR(io.Discard, cfg, "dolt", "wl/alice/w-1", "main")
		if err == nil || !strings.Contains(err.Error(), "gh not found in PATH") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("push failure", func(t *testing.T) {
		installReviewDolt(t)
		installFakeCommand(t, "gh", "#!/bin/sh\nexit 0\n")
		withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error {
			return fmt.Errorf("push failed")
		})

		err := runGitHubPR(io.Discard, cfg, "dolt", "wl/alice/w-go-1", "main")
		if err == nil || !strings.Contains(err.Error(), "pushing to GitHub fork") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCreatePRForBranchDoltHub_ErrorPaths(t *testing.T) {
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
		if _, err := createPRForBranchDoltHub(cfg, "dolt", "wl/alice/w-go-1", "main"); err == nil || !strings.Contains(err.Error(), "DOLTHUB_TOKEN") {
			t.Fatalf("err = %v", err)
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

		if _, err := createPRForBranchDoltHub(cfg, "dolt", "wl/alice/w-go-1", "main"); err == nil || !strings.Contains(err.Error(), "creating DoltHub PR: boom") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestFindExistingPR_ErrorPaths(t *testing.T) {
	t.Run("command failure", func(t *testing.T) {
		ghPath := installFakeCommand(t, "gh", "#!/bin/sh\nexit 1\n")
		if url, number := findExistingPR(ghPath, "org/repo", "alice:wl/alice/w-1"); url != "" || number != "" {
			t.Fatalf("url = %q number = %q", url, number)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		ghPath := installFakeCommand(t, "gh", "#!/bin/sh\nprintf 'not-json\\n'\n")
		if url, number := findExistingPR(ghPath, "org/repo", "alice:wl/alice/w-1"); url != "" || number != "" {
			t.Fatalf("url = %q number = %q", url, number)
		}
	})
}

func TestBranchURLCallback_NilCases(t *testing.T) {
	if cb := branchURLCallback(&federation.Config{ProviderType: "dolthub"}); cb != nil {
		t.Fatal("missing fork info should disable branch URL callback")
	}
	if cb := branchURLCallback(&federation.Config{
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		ProviderType: "file",
	}); cb != nil {
		t.Fatal("unsupported provider should disable branch URL callback")
	}
}

func TestCheckAndClosePRForBranch_DoltHubEdgePaths(t *testing.T) {
	cfg := &federation.Config{
		Upstream:     "hop/wl-commons",
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		ProviderType: "dolthub",
	}

	t.Run("check missing token", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "")
		if got := checkPRForBranch(cfg, "wl/alice/w-1"); got != "" {
			t.Fatalf("got = %q", got)
		}
	})

	t.Run("close invalid upstream", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "token")
		bad := &federation.Config{
			Upstream:     "bad-upstream",
			ForkOrg:      "alice",
			ProviderType: "dolthub",
		}
		if err := closePRForBranch(bad, "wl/alice/w-1"); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("close provider error", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "token")
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				findPRFn: func(string, string, string, string) (string, string) {
					return "https://dolthub.example/pr/1", "1"
				},
				closePRFn: func(string, string, string) error {
					return fmt.Errorf("close failed")
				},
			}
		})
		if err := closePRForBranch(cfg, "wl/alice/w-1"); err == nil || !strings.Contains(err.Error(), "close failed") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCreatePRForBranchGitHub_ExistingPRUpdate(t *testing.T) {
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
			prs: map[string]fakePR{
				"alice:wl/alice/w-go-1": {URL: "https://example/pr/22", Number: "22"},
			},
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
	if url != "https://example/pr/22" {
		t.Fatalf("url = %q", url)
	}
}

func TestCreatePRForBranchGitHub_ErrorPaths(t *testing.T) {
	cfg := &federation.Config{
		Upstream:     "org/repo",
		ForkOrg:      "alice",
		ForkDB:       "repo",
		LocalDir:     t.TempDir(),
		ProviderType: "github",
	}

	t.Run("missing gh", func(t *testing.T) {
		doltPath := installFakeCommand(t, "dolt", "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", filepath.Dir(doltPath))

		if _, err := createPRForBranchGitHub(cfg, "dolt", "wl/alice/w-1", "main"); err == nil || !strings.Contains(err.Error(), "gh not found in PATH") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("push failure", func(t *testing.T) {
		installReviewDolt(t)
		installFakeCommand(t, "gh", "#!/bin/sh\nexit 0\n")
		withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error {
			return fmt.Errorf("push failed")
		})

		if _, err := createPRForBranchGitHub(cfg, "dolt", "wl/alice/w-go-1", "main"); err == nil || !strings.Contains(err.Error(), "pushing to GitHub fork") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestClosePRForBranch_NoOpPaths(t *testing.T) {
	t.Run("github missing gh", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		cfg := &federation.Config{
			Upstream:     "org/repo",
			ForkOrg:      "alice",
			ProviderType: "github",
		}
		if err := closePRForBranch(cfg, "wl/alice/w-1"); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("dolthub missing token", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "")
		cfg := &federation.Config{
			Upstream:     "hop/wl-commons",
			ForkOrg:      "alice",
			ProviderType: "dolthub",
		}
		if err := closePRForBranch(cfg, "wl/alice/w-1"); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}
