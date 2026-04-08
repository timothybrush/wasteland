package dolthubdouble

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandler_QueryMain(t *testing.T) {
	srv, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	err = srv.Seed(SeedRequest{
		Repositories: []RepositorySeed{
			{
				Owner: "e2e",
				DB:    "wl-commons",
				MainSQL: []string{
					"INSERT INTO wanted (id, title, status, type, priority, effort_level, posted_by, created_at, updated_at) VALUES ('w-1', 'hello', 'open', 'bug', 1, 'medium', 'alice', NOW(), NOW())",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ts.URL + "/__dolthub/api/v1alpha1/e2e/wl-commons/main?q=SELECT%20id%2C%20title%20FROM%20wanted")
	if err != nil {
		t.Fatalf("GET query: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	rows, _ := body["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want 1 row", body["rows"])
	}
}

func TestSnapshot(t *testing.T) {
	srv, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	err = srv.Seed(SeedRequest{
		Repositories: []RepositorySeed{
			{
				Owner: "e2e",
				DB:    "wl-commons",
			},
		},
	})
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	snap, err := srv.Snapshot("http://example.test")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snap.Repositories) != 1 {
		t.Fatalf("repositories = %d, want 1", len(snap.Repositories))
	}
}
