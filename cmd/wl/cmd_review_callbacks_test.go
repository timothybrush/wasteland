package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/backend"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestCheckPRForBranch_GitHub(t *testing.T) {
	installFakeCommand(t, "gh", `#!/bin/sh
set -eu
case "$*" in
  "pr list --repo org/repo --head alice:wl/alice/w-1 --state open --json number,url")
    printf '[{"number":11,"url":"https://example/pr/11"}]\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	cfg := &federation.Config{
		Upstream:     "org/repo",
		ForkOrg:      "alice",
		ForkDB:       "fork",
		ProviderType: "github",
	}
	if got := checkPRForBranch(cfg, "wl/alice/w-1"); got != "https://example/pr/11" {
		t.Fatalf("checkPRForBranch() = %q", got)
	}
}

func TestClosePRForBranch_GitHub(t *testing.T) {
	installFakeCommand(t, "gh", `#!/bin/sh
set -eu
case "$*" in
  "pr list --repo org/repo --head alice:wl/alice/w-1 --state open --json number,url")
    printf '[{"number":11,"url":"https://example/pr/11"}]\n'
    ;;
  "api repos/org/repo/pulls/11 -X PATCH --input -")
    printf '{}\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	cfg := &federation.Config{
		Upstream:     "org/repo",
		ForkOrg:      "alice",
		ForkDB:       "fork",
		ProviderType: "github",
	}
	if err := closePRForBranch(cfg, "wl/alice/w-1"); err != nil {
		t.Fatalf("closePRForBranch() error = %v", err)
	}
}

func TestCheckAndClosePRForBranch_DoltHub(t *testing.T) {
	t.Setenv("DOLTHUB_TOKEN", "token")

	cfg := &federation.Config{
		Upstream:     "hop/wl-commons",
		ForkOrg:      "alice",
		ForkDB:       "fork",
		ProviderType: "dolthub",
	}

	t.Run("check", func(t *testing.T) {
		withDoltHubPRProviderOverride(t, func(token string) doltHubPRProvider {
			if token != "token" {
				t.Fatalf("token = %q", token)
			}
			return fakeDoltHubPRProvider{
				findPRFn: func(upstreamOrg, db, forkOrg, fromBranch string) (string, string) {
					if upstreamOrg != "hop" || db != "wl-commons" || forkOrg != "alice" || fromBranch != "wl/alice/w-1" {
						t.Fatalf("got %q %q %q %q", upstreamOrg, db, forkOrg, fromBranch)
					}
					return "https://dolthub.example/pr/11", "11"
				},
			}
		})

		if got := checkPRForBranch(cfg, "wl/alice/w-1"); got != "https://dolthub.example/pr/11" {
			t.Fatalf("checkPRForBranch() = %q", got)
		}
	})

	t.Run("close", func(t *testing.T) {
		var closed string
		withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
			return fakeDoltHubPRProvider{
				findPRFn: func(string, string, string, string) (string, string) {
					return "https://dolthub.example/pr/11", "11"
				},
				closePRFn: func(upstreamOrg, db, prID string) error {
					if upstreamOrg != "hop" || db != "wl-commons" {
						t.Fatalf("got %q %q", upstreamOrg, db)
					}
					closed = prID
					return nil
				},
			}
		})

		if err := closePRForBranch(cfg, "wl/alice/w-1"); err != nil {
			t.Fatalf("closePRForBranch() error = %v", err)
		}
		if closed != "11" {
			t.Fatalf("closed = %q", closed)
		}
	})
}

func TestBranchURLCallback(t *testing.T) {
	dolthub := branchURLCallback(&federation.Config{
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		ProviderType: "dolthub",
	})
	if got := dolthub("wl/alice/w-1"); !strings.Contains(got, "alice/wl-commons") || !strings.Contains(got, "wl%2Falice%2Fw-1") {
		t.Fatalf("dolthub URL = %q", got)
	}

	github := branchURLCallback(&federation.Config{
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		ProviderType: "github",
	})
	if got := github("wl/alice/w-1"); !strings.Contains(got, "github.com/alice/wl-commons/tree/") {
		t.Fatalf("github URL = %q", got)
	}
}

func TestPendingDetailLoaderHelpers(t *testing.T) {
	if cb := pendingItemLoaderCallback(&federation.Config{Backend: federation.BackendLocal}); cb != nil {
		t.Fatal("local backend should not create pending item loader")
	}
	if cb := pendingItemLoaderCallback(&federation.Config{Backend: federation.BackendRemote, ProviderType: "github"}); cb != nil {
		t.Fatal("non-dolthub provider should not create pending item loader")
	}
	if cb := pendingDetailLoaderCallback(&federation.Config{Backend: federation.BackendLocal}); cb != nil {
		t.Fatal("local backend should not create pending detail loader")
	}
	if cb := pendingDetailLoaderCallback(&federation.Config{Backend: federation.BackendRemote, ProviderType: "github"}); cb != nil {
		t.Fatal("non-dolthub provider should not create pending detail loader")
	}

	itemLoader := pendingItemLoader("hop", "wl-commons", federation.ModePR, "token")
	if _, err := itemLoader("w-1", sdk.PendingItem{}); err == nil || !strings.Contains(err.Error(), "missing fork owner or branch") {
		t.Fatalf("err = %v", err)
	}

	loader := pendingDetailLoader("hop", "wl-commons", federation.ModePR, "token")
	if _, _, _, err := loader("w-1", sdk.PendingItem{}); err == nil || !strings.Contains(err.Error(), "missing fork owner or branch") {
		t.Fatalf("err = %v", err)
	}
}

func TestCloseUpstreamPRCallback(t *testing.T) {
	if cb := closeUpstreamPRCallback(&federation.Config{ProviderType: "github"}); cb != nil {
		t.Fatal("github provider should not wire closeUpstreamPR callback")
	}
}

func TestPendingDetailLoader_LoadsForkBranchDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")

		var resp map[string]any
		switch {
		case strings.Contains(q, "LEFT JOIN completions") && strings.Contains(q, "WHERE w.id='w-1'"):
			resp = map[string]any{
				"query_execution_status": "Success",
				"schema_fragment": []map[string]string{
					{"columnName": "id", "columnType": "varchar(20)"},
					{"columnName": "title", "columnType": "varchar(255)"},
					{"columnName": "description", "columnType": "text"},
					{"columnName": "project", "columnType": "varchar(255)"},
					{"columnName": "type", "columnType": "varchar(255)"},
					{"columnName": "priority", "columnType": "int"},
					{"columnName": "tags", "columnType": "text"},
					{"columnName": "posted_by", "columnType": "varchar(255)"},
					{"columnName": "claimed_by", "columnType": "varchar(255)"},
					{"columnName": "status", "columnType": "varchar(32)"},
					{"columnName": "effort_level", "columnType": "varchar(32)"},
					{"columnName": "created_at", "columnType": "varchar(64)"},
					{"columnName": "updated_at", "columnType": "varchar(64)"},
					{"columnName": "completion_id", "columnType": "varchar(20)"},
					{"columnName": "completion_wanted_id", "columnType": "varchar(20)"},
					{"columnName": "completed_by", "columnType": "varchar(255)"},
					{"columnName": "evidence", "columnType": "text"},
					{"columnName": "completion_stamp_id", "columnType": "varchar(64)"},
					{"columnName": "validated_by", "columnType": "varchar(255)"},
					{"columnName": "stamp_record_id", "columnType": "varchar(20)"},
					{"columnName": "stamp_author", "columnType": "varchar(255)"},
					{"columnName": "stamp_subject", "columnType": "varchar(255)"},
					{"columnName": "stamp_valence", "columnType": "text"},
					{"columnName": "stamp_severity", "columnType": "varchar(32)"},
					{"columnName": "stamp_context_id", "columnType": "varchar(255)"},
					{"columnName": "stamp_context_type", "columnType": "varchar(255)"},
					{"columnName": "stamp_skill_tags", "columnType": "text"},
					{"columnName": "stamp_message", "columnType": "text"},
				},
				"rows": []map[string]string{{
					"id":                   "w-1",
					"title":                "Fix auth",
					"description":          "",
					"project":              "",
					"type":                 "",
					"priority":             "2",
					"tags":                 "",
					"posted_by":            "alice",
					"claimed_by":           "alice",
					"status":               "in_review",
					"effort_level":         "medium",
					"created_at":           "",
					"updated_at":           "",
					"completion_id":        "c-1",
					"completion_wanted_id": "w-1",
					"completed_by":         "alice",
					"evidence":             "https://example/evidence",
					"completion_stamp_id":  "s-1",
					"validated_by":         "reviewer",
					"stamp_record_id":      "s-1",
					"stamp_author":         "reviewer",
					"stamp_subject":        "alice",
					"stamp_valence":        `{"quality":1,"reliability":1}`,
					"stamp_severity":       "info",
					"stamp_context_id":     "",
					"stamp_context_type":   "",
					"stamp_skill_tags":     "",
					"stamp_message":        "looks good",
				}},
			}
		default:
			http.Error(w, "unexpected query", http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	oldBase := backend.DoltHubAPIBase
	backend.DoltHubAPIBase = srv.URL
	t.Cleanup(func() {
		backend.DoltHubAPIBase = oldBase
	})

	loader := pendingDetailLoader("hop", "wl-commons", federation.ModePR, "token")
	item, completion, stamp, err := loader("w-1", sdk.PendingItem{
		ForkOwner: "alice",
		Branch:    "wl/alice/w-1",
	})
	if err != nil {
		t.Fatalf("loader() error = %v", err)
	}
	if item == nil || item.Title != "Fix auth" {
		t.Fatalf("item = %+v", item)
	}
	if completion == nil || completion.Evidence != "https://example/evidence" {
		t.Fatalf("completion = %+v", completion)
	}
	if stamp == nil || stamp.ID != "s-1" {
		t.Fatalf("stamp = %+v", stamp)
	}
}

func TestCloseUpstreamPRCallback_DoltHub(t *testing.T) {
	t.Setenv("DOLTHUB_TOKEN", "token")

	var closedPR string
	withDoltHubPRProviderOverride(t, func(token string) doltHubPRProvider {
		if token != "token" {
			t.Fatalf("token = %q", token)
		}
		return fakeDoltHubPRProvider{
			closePRFn: func(upstreamOrg, db, prID string) error {
				if upstreamOrg != "hop" || db != "wl-commons" {
					t.Fatalf("got %q %q", upstreamOrg, db)
				}
				closedPR = prID
				return nil
			},
		}
	})

	cb := closeUpstreamPRCallback(&federation.Config{
		Upstream:     "hop/wl-commons",
		ProviderType: "dolthub",
	})
	if cb == nil {
		t.Fatal("callback is nil")
	}
	if err := cb("https://www.dolthub.com/repositories/hop/wl-commons/pulls/123"); err != nil {
		t.Fatalf("callback() error = %v", err)
	}
	if closedPR != "123" {
		t.Fatalf("closedPR = %q", closedPR)
	}
	if err := cb("https://www.dolthub.com/repositories/hop/wl-commons/not-a-pull"); err == nil || !strings.Contains(err.Error(), "cannot extract PR ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestListPendingItemsFromPRs_SelectsProviderCallbacks(t *testing.T) {
	t.Run("dolthub", func(t *testing.T) {
		t.Setenv("DOLTHUB_TOKEN", "token")
		withPendingWantedStatesOverride(t, func(upstreamOrg, db, token string) (map[string][]remote.PendingWantedState, error) {
			if upstreamOrg != "hop" || db != "wl-commons" || token != "token" {
				t.Fatalf("got %q %q %q", upstreamOrg, db, token)
			}
			return map[string][]remote.PendingWantedState{
				"w-1": {{
					RigHandle: "alice",
					Branch:    "wl/alice/w-1",
				}},
			}, nil
		})

		cb := listPendingItemsFromPRs(&federation.Config{
			Upstream:     "hop/wl-commons",
			ProviderType: "dolthub",
		})
		if cb == nil {
			t.Fatal("callback is nil")
		}

		pending, err := cb()
		if err != nil {
			t.Fatalf("callback() error = %v", err)
		}
		if len(pending["w-1"]) != 1 || pending["w-1"][0].Branch != "wl/alice/w-1" {
			t.Fatalf("pending = %+v", pending)
		}
	})

	t.Run("github", func(t *testing.T) {
		installFakeCommand(t, "gh", `#!/bin/sh
set -eu
case "$*" in
  "api --paginate repos/org/repo/pulls?state=open&per_page=100")
    printf '[{"head":{"ref":"wl/alice/w-9"},"title":"Review w-9","user":{"login":"alice"}}]\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

		cb := listPendingItemsFromPRs(&federation.Config{
			Upstream:     "org/repo",
			ProviderType: "github",
		})
		if cb == nil {
			t.Fatal("callback is nil")
		}

		pending, err := cb()
		if err != nil {
			t.Fatalf("callback() error = %v", err)
		}
		if len(pending["w-9"]) != 1 || pending["w-9"][0].RigHandle != "alice" {
			t.Fatalf("pending = %+v", pending)
		}
	})
}

func TestListBranchesWithTimeout(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
printf 'name\nwl/alice/w-1\nwl/alice/w-2\n'
`)
	branches := listBranchesWithTimeout(t.TempDir())
	if len(branches) != 2 || branches[1] != "wl/alice/w-2" {
		t.Fatalf("branches = %v", branches)
	}
}

func TestCreatePRForBranch_UnsupportedProvider(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  remote)
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
`)

	cfg := &federation.Config{
		Upstream:     "hop/wl-commons",
		LocalDir:     t.TempDir(),
		ProviderType: "file",
	}
	if _, err := createPRForBranch(cfg, "wl/alice/w-1"); err == nil || !strings.Contains(err.Error(), "does not support pull requests") {
		t.Fatalf("err = %v", err)
	}
}
