package pile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/backend"
)

func TestNewAndNewDefault(t *testing.T) {
	client := New("token-123", "gastownhall", "the-pile")
	if client.org != "gastownhall" || client.db != "the-pile" || client.branch != "main" || client.token != "token-123" {
		t.Fatalf("New() client = %+v", client)
	}
	if client.client == nil {
		t.Fatal("New() should initialize http client")
	}

	def := NewDefault()
	if def.org != "hop" || def.db != "the-pile" || def.branch != "main" || def.token != "" {
		t.Fatalf("NewDefault() client = %+v", def)
	}
}

func TestQueryRawAndQueryRows(t *testing.T) {
	oldBase := backend.DoltHubAPIBase
	defer func() { backend.DoltHubAPIBase = oldBase }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token secret-token" {
			t.Fatalf("authorization = %q, want token header", got)
		}
		if !strings.Contains(r.URL.Path, "/hop/the-pile/main") {
			t.Fatalf("path = %q, want hop/the-pile/main", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "SELECT handle FROM pile" {
			t.Fatalf("query = %q, want SQL", r.URL.Query().Get("q"))
		}
		resp := map[string]any{
			"query_execution_status": "Success",
			"rows": []any{
				map[string]any{"handle": "alice"},
				123, // ignored malformed row payload for QueryRows
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	backend.DoltHubAPIBase = ts.URL
	client := New("secret-token", "hop", "the-pile")
	client.client = ts.Client()

	body, err := client.queryRaw("SELECT handle FROM pile")
	if err != nil {
		t.Fatalf("queryRaw() error = %v", err)
	}
	if !strings.Contains(string(body), `"query_execution_status":"Success"`) {
		t.Fatalf("queryRaw() body = %q", string(body))
	}

	rows, err := client.QueryRows("SELECT handle FROM pile")
	if err != nil {
		t.Fatalf("QueryRows() error = %v", err)
	}
	if len(rows) != 1 || rows[0]["handle"] != "alice" {
		t.Fatalf("QueryRows() = %+v, want one alice row", rows)
	}
}

func TestQueryRawAndQueryRows_ErrorPaths(t *testing.T) {
	oldBase := backend.DoltHubAPIBase
	defer func() { backend.DoltHubAPIBase = oldBase }()

	t.Run("http failure is truncated", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, strings.Repeat("x", 260), http.StatusBadGateway)
		}))
		defer ts.Close()

		backend.DoltHubAPIBase = ts.URL
		client := New("", "hop", "the-pile")
		client.client = ts.Client()

		_, err := client.queryRaw("SELECT 1")
		if err == nil || !strings.Contains(err.Error(), "DoltHub API returned 502") || !strings.Contains(err.Error(), "...") {
			t.Fatalf("queryRaw() error = %v, want truncated HTTP error", err)
		}
	})

	t.Run("query status error surfaces message", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			resp := map[string]any{
				"query_execution_status":  "Error",
				"query_execution_message": "syntax exploded",
				"rows":                    []any{},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer ts.Close()

		backend.DoltHubAPIBase = ts.URL
		client := New("", "hop", "the-pile")
		client.client = ts.Client()

		_, err := client.QueryRows("SELECT 1")
		if err == nil || !strings.Contains(err.Error(), "syntax exploded") {
			t.Fatalf("QueryRows() error = %v, want query message", err)
		}
	})
}

func TestTruncateAndProfileHelpers(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate(short) = %q, want short", got)
	}
	if got := truncate("abcdefghijklmnopqrstuvwxyz", 5); got != "abcde..." {
		t.Fatalf("truncate(long) = %q, want abcde...", got)
	}

	profile := &Profile{}
	parseStamps([]map[string]any{
		{
			"skill_tags": `["go","systems-programming"]`,
			"valence":    `{"quality":4,"reliability":5,"creativity":2}`,
			"confidence": "0.9",
			"message":    "Strong Go systems work",
		},
		{
			"skill_tags": `["backend-platform"]`,
			"valence":    `{"quality":3,"reliability":4,"creativity":3}`,
			"confidence": 0.8,
			"message":    map[string]any{"note": "structured"},
		},
		{
			"skill_tags": `["mentorship"]`,
			"valence":    `{"quality":5,"reliability":4,"creativity":4}`,
			"confidence": "0.7",
			"message":    "helps others ship",
		},
	}, profile)

	if len(profile.Languages) != 1 || profile.Languages[0].Name != "go" {
		t.Fatalf("Languages = %+v, want go language stamp", profile.Languages)
	}
	if len(profile.Domains) != 1 || profile.Domains[0].Name != "backend-platform" {
		t.Fatalf("Domains = %+v, want backend domain stamp", profile.Domains)
	}
	if len(profile.Capabilities) != 1 || profile.Capabilities[0].Name != "mentorship" {
		t.Fatalf("Capabilities = %+v, want mentorship capability stamp", profile.Capabilities)
	}
	if !strings.Contains(profile.Domains[0].Message, `"note":"structured"`) {
		t.Fatalf("domain message = %q, want marshaled object", profile.Domains[0].Message)
	}

	if !isDomainTag("backend-platform") {
		t.Fatal("isDomainTag() should accept prefixed domain tags")
	}
	if isDomainTag("mentorship") {
		t.Fatal("isDomainTag() should reject capability tags")
	}
	if got := toString(map[string]any{"x": 1}); got != `{"x":1}` {
		t.Fatalf("toString(map) = %q, want JSON", got)
	}
}
