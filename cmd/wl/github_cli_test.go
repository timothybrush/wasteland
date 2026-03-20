package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestGHCLIClient_Methods(t *testing.T) {
	logPath := t.TempDir() + "/gh.log"
	t.Setenv("GH_LOG", logPath)
	installFakeCommand(t, "gh", `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_LOG"
case "$*" in
  "pr list --repo org/repo --head alice:wl/alice/w-1 --state open --json number,url")
    printf '[{"number":7,"url":"https://example/pr/7"}]\n'
    ;;
  "api repos/org/repo/pulls/7/reviews")
    printf '[{"user":{"login":"alice"},"state":"APPROVED"}]\n'
    ;;
  "api repos/org/repo/pulls/7/reviews -X POST --input -")
    printf '{}\n'
    ;;
  "api repos/org/repo/pulls/7 -X PATCH --input -")
    printf '{}\n'
    ;;
  "api repos/org/repo/issues/7/comments -X POST --input -")
    printf '{}\n'
    ;;
  "api repos/fork/repo/git/refs/heads/wl/alice/w-1 -X DELETE")
    printf '{}\n'
    ;;
  "api repos/org/repo/git/ref/heads/main")
    printf '{"object":{"sha":"ref-sha"}}\n'
    ;;
  "api repos/org/repo/git/commits/ref-sha")
    printf '{"tree":{"sha":"tree-sha"}}\n'
    ;;
  "api repos/org/repo/git/blobs -X POST --input -")
    printf '{"sha":"blob-sha"}\n'
    ;;
  "api repos/org/repo/git/trees -X POST --input -")
    printf '{"sha":"new-tree-sha"}\n'
    ;;
  "api repos/org/repo/git/commits -X POST --input -")
    printf '{"sha":"commit-sha"}\n'
    ;;
  "api repos/org/repo/git/refs -X POST --input -")
    printf '{}\n'
    ;;
  "api repos/org/repo/git/refs/heads/main -X PATCH --input -")
    printf '{}\n'
    ;;
  "api repos/org/repo/pulls -X POST --input -")
    printf '{"html_url":"https://example/pr/8"}\n'
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 1
    ;;
esac
`)

	client := newGHClient("gh")

	if url, num := client.FindPR("org/repo", "alice:wl/alice/w-1"); url != "https://example/pr/7" || num != "7" {
		t.Fatalf("FindPR() = %q, %q", url, num)
	}
	if err := client.SubmitReview("org/repo", "7", "APPROVE", "looks good"); err != nil {
		t.Fatalf("SubmitReview() error = %v", err)
	}
	if data, err := client.ListReviews("org/repo", "7"); err != nil || !strings.Contains(string(data), "APPROVED") {
		t.Fatalf("ListReviews() = %q, %v", string(data), err)
	}
	if err := client.ClosePR("org/repo", "7"); err != nil {
		t.Fatalf("ClosePR() error = %v", err)
	}
	if err := client.AddComment("org/repo", "7", "merged"); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	if err := client.DeleteRef("fork/repo", "heads/wl/alice/w-1"); err != nil {
		t.Fatalf("DeleteRef() error = %v", err)
	}
	if sha, err := client.GetRef("org/repo", "heads/main"); err != nil || sha != "ref-sha" {
		t.Fatalf("GetRef() = %q, %v", sha, err)
	}
	if sha, err := client.GetCommitTree("org/repo", "ref-sha"); err != nil || sha != "tree-sha" {
		t.Fatalf("GetCommitTree() = %q, %v", sha, err)
	}
	if sha, err := client.CreateBlob("org/repo", "hello", "utf-8"); err != nil || sha != "blob-sha" {
		t.Fatalf("CreateBlob() = %q, %v", sha, err)
	}
	if sha, err := client.CreateTree("org/repo", "tree-sha", []TreeEntry{{Path: "README.md", Mode: "100644", Type: "blob", SHA: "blob-sha"}}); err != nil || sha != "new-tree-sha" {
		t.Fatalf("CreateTree() = %q, %v", sha, err)
	}
	if sha, err := client.CreateCommit("org/repo", "commit", "tree-sha", []string{"ref-sha"}); err != nil || sha != "commit-sha" {
		t.Fatalf("CreateCommit() = %q, %v", sha, err)
	}
	if err := client.CreateRef("org/repo", "refs/heads/main", "commit-sha"); err != nil {
		t.Fatalf("CreateRef() error = %v", err)
	}
	if err := client.UpdateRef("org/repo", "heads/main", "commit-sha", true); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	if url, err := client.CreatePR("org/repo", "title", "body", "head", "main"); err != nil || url != "https://example/pr/8" {
		t.Fatalf("CreatePR() = %q, %v", url, err)
	}
	if err := client.UpdatePR("org/repo", "7", map[string]string{"title": "updated"}); err != nil {
		t.Fatalf("UpdatePR() error = %v", err)
	}
}

func TestNewGHClient(t *testing.T) {
	client := newGHClient("/tmp/gh")
	if client.ghPath != "/tmp/gh" {
		t.Fatalf("ghPath = %q", client.ghPath)
	}
}

func TestGHCLIClient_ParseErrors(t *testing.T) {
	installFakeCommand(t, "gh", `#!/bin/sh
set -eu
case "$*" in
  "api repos/org/repo/git/ref/heads/main"|"api repos/org/repo/git/commits/ref-sha"|"api repos/org/repo/git/blobs -X POST --input -"|"api repos/org/repo/git/trees -X POST --input -"|"api repos/org/repo/git/commits -X POST --input -"|"api repos/org/repo/pulls -X POST --input -")
    printf 'not-json\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	client := newGHClient("gh")
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "GetRef",
			run: func() error {
				_, err := client.GetRef("org/repo", "heads/main")
				return err
			},
		},
		{
			name: "GetCommitTree",
			run: func() error {
				_, err := client.GetCommitTree("org/repo", "ref-sha")
				return err
			},
		},
		{
			name: "CreateBlob",
			run: func() error {
				_, err := client.CreateBlob("org/repo", "hello", "utf-8")
				return err
			},
		},
		{
			name: "CreateTree",
			run: func() error {
				_, err := client.CreateTree("org/repo", "tree-sha", []TreeEntry{{Path: "README.md", Mode: "100644", Type: "blob", SHA: "blob-sha"}})
				return err
			},
		},
		{
			name: "CreateCommit",
			run: func() error {
				_, err := client.CreateCommit("org/repo", "commit", "tree-sha", []string{"ref-sha"})
				return err
			},
		},
		{
			name: "CreatePR",
			run: func() error {
				_, err := client.CreatePR("org/repo", "title", "body", "head", "main")
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || !strings.Contains(err.Error(), "parsing") {
				t.Fatalf("%s error = %v", tc.name, err)
			}
		})
	}
}

func TestGHCLIClient_FindPRAndListReviews_CommandErrors(t *testing.T) {
	ghPath := installFakeCommand(t, "gh", "#!/bin/sh\nexit 1\n")
	client := newGHClient(ghPath)

	if url, number := client.FindPR("org/repo", "alice:wl/alice/w-1"); url != "" || number != "" {
		t.Fatalf("FindPR() = %q, %q", url, number)
	}
	if _, err := client.ListReviews("org/repo", "7"); err == nil || !strings.Contains(err.Error(), "gh api GET") {
		t.Fatalf("ListReviews() error = %v", err)
	}
}

func TestGHCLIClient_UpdatePR_Error(t *testing.T) {
	installFakeCommand(t, "gh", `#!/bin/sh
set -eu
printf 'boom\n' >&2
exit 1
`)

	client := newGHClient("gh")
	err := client.UpdatePR("org/repo", "7", map[string]string{"title": "updated"})
	if err == nil || !strings.Contains(err.Error(), "gh api PATCH") {
		t.Fatalf("UpdatePR() error = %v", err)
	}
	if !strings.Contains(fmt.Sprint(err), "pulls/7") {
		t.Fatalf("error missing endpoint: %v", err)
	}
}
