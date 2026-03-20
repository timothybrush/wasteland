package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/backend"
	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
)

func TestRunReview_RemoteDiffUsesRemoteDB(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")

		switch {
		case strings.Contains(q, "dolt_diff_stat("):
			resp := map[string]any{
				"query_execution_status": "Success",
				"schema_fragment": []map[string]string{
					{"columnName": "table_name", "columnType": "varchar(255)"},
					{"columnName": "rows_added", "columnType": "int"},
					{"columnName": "rows_modified", "columnType": "int"},
					{"columnName": "rows_deleted", "columnType": "int"},
				},
				"rows": []map[string]string{
					{"table_name": "wanted", "rows_added": "0", "rows_modified": "1", "rows_deleted": "0"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case strings.Contains(q, "dolt_diff("):
			resp := map[string]any{
				"query_execution_status": "Success",
				"schema_fragment": []map[string]string{
					{"columnName": "diff_type", "columnType": "varchar(20)"},
					{"columnName": "from_id", "columnType": "varchar(20)"},
					{"columnName": "to_id", "columnType": "varchar(20)"},
					{"columnName": "from_status", "columnType": "varchar(20)"},
					{"columnName": "to_status", "columnType": "varchar(20)"},
				},
				"rows": []map[string]string{
					{
						"diff_type":   "modified",
						"from_id":     "w-1",
						"to_id":       "w-1",
						"from_status": "open",
						"to_status":   "claimed",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.Error(w, "unexpected query", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)

	oldBase := backend.DoltHubAPIBase
	backend.DoltHubAPIBase = srv.URL
	t.Cleanup(func() {
		backend.DoltHubAPIBase = oldBase
	})

	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return backend.NewRemoteDBWithClient(srv.Client(), "hop", "wl-commons", "alice", "wl-commons", federation.ModePR), nil
	})

	var stdout bytes.Buffer
	if err := runReview(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "wl/alice/w-1", false, false, false, false); err != nil {
		t.Fatalf("runReview(remote diff) error = %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "wanted") || !strings.Contains(out, "modified") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRunReview_RemoteCreatePR(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DOLTHUB_TOKEN", "token")
	saveTestConfig(t, &federation.Config{
		Upstream:     "hop/wl-commons",
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		RigHandle:    "alice",
		Backend:      federation.BackendRemote,
		ProviderType: "dolthub",
		JoinedAt:     time.Now(),
	})

	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(query, ref string) (string, error) {
				if ref != "wl/alice/w-1" || !strings.Contains(query, "FROM wanted WHERE id='w-1'") {
					t.Fatalf("query = %q ref = %q", query, ref)
				}
				return "id,title,description,project,type,priority,tags,posted_by,claimed_by,status,effort_level,created_at,updated_at\nw-1,Fix auth,,,,2,,alice,,claimed,medium,,\n", nil
			},
		}, nil
	})
	withDoltHubPRProviderOverride(t, func(token string) doltHubPRProvider {
		if token != "token" {
			t.Fatalf("token = %q", token)
		}
		return fakeDoltHubPRProvider{
			createPRFn: func(forkOrg, upstreamOrg, db, branch, title, body string) (string, error) {
				if forkOrg != "alice" || upstreamOrg != "hop" || db != "wl-commons" {
					t.Fatalf("got %q %q %q", forkOrg, upstreamOrg, db)
				}
				if branch != "wl/alice/w-1" || title != "[wl] Fix auth" || body != "" {
					t.Fatalf("got %q %q %q", branch, title, body)
				}
				return "https://dolthub.example/pr/remote", nil
			},
		}
	})

	var stdout bytes.Buffer
	if err := runReview(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "wl/alice/w-1", false, false, false, true); err != nil {
		t.Fatalf("runReview(remote create-pr) error = %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "https://dolthub.example/pr/remote") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRunReview_LocalCreatePRDoltHub(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DOLTHUB_TOKEN", "token")
	saveTestConfig(t, &federation.Config{
		Upstream:     "hop/wl-commons",
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		LocalDir:     t.TempDir(),
		RigHandle:    "alice",
		Backend:      federation.BackendLocal,
		ProviderType: "dolthub",
		JoinedAt:     time.Now(),
	})
	installReviewDolt(t)

	withPushBranchOverride(t, func(string, string, string, bool, io.Writer) error { return nil })
	withDoltHubPRProviderOverride(t, func(string) doltHubPRProvider {
		return fakeDoltHubPRProvider{
			createPRFn: func(_, _, _, branch, title, body string) (string, error) {
				if branch != "wl/alice/w-go-1" || !strings.Contains(title, "Fix auth") || !strings.Contains(body, "UPDATE wanted") {
					t.Fatalf("got %q %q %q", branch, title, body)
				}
				return "https://dolthub.example/pr/local", nil
			},
		}
	})

	var stdout bytes.Buffer
	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")
	if err := runReview(cmd, &stdout, io.Discard, "wl/alice/w-go-1", false, false, false, true); err != nil {
		t.Fatalf("runReview(local create-pr) error = %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "https://dolthub.example/pr/local") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRunReview_LocalCreatePRUnsupportedProvider(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:     "hop/wl-commons",
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		LocalDir:     t.TempDir(),
		RigHandle:    "alice",
		Backend:      federation.BackendLocal,
		ProviderType: "file",
		JoinedAt:     time.Now(),
	})
	installReviewDolt(t)

	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")
	err := runReview(cmd, io.Discard, io.Discard, "wl/alice/w-go-1", false, false, false, true)
	if err == nil || !strings.Contains(err.Error(), `provider "file" does not support pull requests`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunReview_LocalMissingDolt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  t.TempDir(),
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})

	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")
	err := runReview(cmd, io.Discard, io.Discard, "wl/alice/w-1", false, false, false, false)
	if err == nil || !strings.Contains(err.Error(), "dolt not found in PATH") {
		t.Fatalf("err = %v", err)
	}
}
