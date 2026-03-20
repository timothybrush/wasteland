package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestCachedEndpoint_StartStop(t *testing.T) {
	var calls atomic.Int32
	ready := make(chan struct{}, 1)
	cache := NewCachedEndpoint(func() ([]byte, error) {
		if calls.Add(1) == 1 {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
		return []byte(`{"ok":true}`), nil
	}, 5*time.Millisecond)

	cache.Start()
	defer cache.Stop()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for background refresh")
	}

	if got := string(cache.Get()); got != `{"ok":true}` {
		t.Fatalf("cache.Get() = %q, want %q", got, `{"ok":true}`)
	}

	cache.Stop()
}

func TestScoreboardHandler_DataUnavailable(t *testing.T) {
	srv := New(newTestClient(newFakeDB()))
	srv.SetScoreboard(NewCachedEndpoint(func() ([]byte, error) {
		return nil, errors.New("refresh failed")
	}, time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/api/scoreboard", nil)
	rec := httptest.NewRecorder()
	srv.ScoreboardHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if !strings.Contains(resp.Error, "scoreboard data unavailable") {
		t.Fatalf("error = %q, want scoreboard data unavailable", resp.Error)
	}
}

func TestNewHostedWorkspace_ConfigIncludesWorkspaceAndActiveUpstream(t *testing.T) {
	db := newFakeDB()
	client := newTestClient(db)
	workspace := sdk.NewWorkspace("alice")
	workspace.Add(sdk.UpstreamInfo{
		Upstream: "hop/wl-commons",
		ForkOrg:  "alice",
		ForkDB:   "wl-commons",
		Mode:     "pr",
	}, client)

	srv := NewHostedWorkspace(
		func(*http.Request) (*sdk.Client, error) { return client, nil },
		func(*http.Request) (*sdk.Workspace, error) { return workspace, nil },
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/config", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Wasteland", "hop/wl-commons")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var cfg ConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !cfg.Hosted || !cfg.Connected {
		t.Fatalf("expected hosted connected config, got hosted=%v connected=%v", cfg.Hosted, cfg.Connected)
	}
	if cfg.Upstream != "hop/wl-commons" {
		t.Fatalf("upstream = %q, want %q", cfg.Upstream, "hop/wl-commons")
	}
	if len(cfg.Upstreams) != 1 {
		t.Fatalf("upstreams len = %d, want 1", len(cfg.Upstreams))
	}
	if cfg.Upstreams[0].ForkOrg != "alice" || cfg.Upstreams[0].Mode != "pr" {
		t.Fatalf("unexpected upstream info: %+v", cfg.Upstreams[0])
	}
}

func TestSetProfileQuerier_OverridesProfileSource(t *testing.T) {
	sheetJSON := `{"identity":{"display_name":"Injected"},"value_dimensions":{"quality":0.7}}`
	pq := &fakePileQuerier{rows: map[string][]map[string]any{
		"SELECT handle": {
			{"handle": "test", "source": "github", "sheet_json": sheetJSON, "confidence": "0.9", "created_at": "2024-01-01"},
		},
		"SELECT skill_tags": {},
	}}

	srv := &Server{
		clientFunc: func(*http.Request) (*sdk.Client, error) { return nil, nil },
		mux:        http.NewServeMux(),
	}
	srv.registerRoutes()
	srv.SetProfileQuerier(pq)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/profile/test")
	if err != nil {
		t.Fatalf("GET /api/profile/test: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var profile struct {
		Handle      string `json:"handle"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if profile.Handle != "test" {
		t.Fatalf("handle = %q, want %q", profile.Handle, "test")
	}
}

func TestBrowse_UsesStaleCacheOnTransientError(t *testing.T) {
	db := newFakeDB()
	db.items["w-1"] = &fakeItem{id: "w-1", title: "Fix bug", status: "open", postedBy: "alice", effortLevel: "small"}
	srv := New(newTestClient(db))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	var warm BrowseResponse
	r := getJSON(t, ts, "/api/wanted", &warm)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("warm browse status = %d, want 200", r.StatusCode)
	}

	srv.browseCache.mu.Lock()
	entry := srv.browseCache.entries["alice:"]
	if entry == nil {
		srv.browseCache.mu.Unlock()
		t.Fatal("expected warm browse cache entry")
	}
	entry.storedAt = time.Now().Add(-time.Minute)
	srv.browseCache.mu.Unlock()

	db.queryErrors = map[string]error{
		"FROM wanted": errors.New("no such repository"),
	}

	var stale BrowseResponse
	r = getJSON(t, ts, "/api/wanted", &stale)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("stale browse status = %d, want 200", r.StatusCode)
	}
	if got := r.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-store")
	}
	if len(stale.Items) != 1 || stale.Items[0].ID != "w-1" {
		t.Fatalf("unexpected stale items: %+v", stale.Items)
	}
	if !strings.Contains(stale.Warning, "Showing cached data") {
		t.Fatalf("warning = %q, want stale-data warning", stale.Warning)
	}
}

func TestBrowse_AuthErrorDoesNotServeStaleCache(t *testing.T) {
	db := newFakeDB()
	db.items["w-1"] = &fakeItem{id: "w-1", title: "Fix bug", status: "open", postedBy: "alice", effortLevel: "small"}
	srv := New(newTestClient(db))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	var warm BrowseResponse
	r := getJSON(t, ts, "/api/wanted", &warm)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("warm browse status = %d, want 200", r.StatusCode)
	}

	srv.browseCache.mu.Lock()
	entry := srv.browseCache.entries["alice:"]
	if entry == nil {
		srv.browseCache.mu.Unlock()
		t.Fatal("expected warm browse cache entry")
	}
	entry.storedAt = time.Now().Add(-time.Minute)
	srv.browseCache.mu.Unlock()

	db.queryErrors = map[string]error{
		"FROM wanted": errors.New("invalid authorization"),
	}

	var resp ErrorResponse
	r = getJSON(t, ts, "/api/wanted", &resp)
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", r.StatusCode, http.StatusUnauthorized)
	}
	if !strings.Contains(resp.Error, "please reconnect") {
		t.Fatalf("error = %q, want reconnect hint", resp.Error)
	}
}

func TestDetail_UsesStaleCacheOnTransientError(t *testing.T) {
	db := newFakeDB()
	db.items["w-1"] = &fakeItem{id: "w-1", title: "Fix bug", status: "open", postedBy: "alice", effortLevel: "small"}
	srv := New(newTestClient(db))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	var warm DetailResponse
	r := getJSON(t, ts, "/api/wanted/w-1", &warm)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("warm detail status = %d, want 200", r.StatusCode)
	}

	srv.detailCache.mu.Lock()
	entry := srv.detailCache.entries["alice:w-1"]
	if entry == nil {
		srv.detailCache.mu.Unlock()
		t.Fatal("expected warm detail cache entry")
	}
	entry.storedAt = time.Now().Add(-time.Minute)
	srv.detailCache.mu.Unlock()

	db.queryErrors = map[string]error{
		"WHERE id='w-1'": errors.New("no such repository"),
	}

	var stale DetailResponse
	r = getJSON(t, ts, "/api/wanted/w-1", &stale)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("stale detail status = %d, want 200", r.StatusCode)
	}
	if got := r.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-store")
	}
	if stale.Item == nil || stale.Item.ID != "w-1" {
		t.Fatalf("unexpected stale detail: %+v", stale.Item)
	}
}

func TestWriteUpstreamError_ClassifiesErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		want   int
		substr string
	}{
		{
			name:   "auth",
			err:    errors.New("invalid authorization"),
			want:   http.StatusUnauthorized,
			substr: "please reconnect",
		},
		{
			name:   "token permission",
			err:    errors.New("API token is not allowed to operate on this resource"),
			want:   http.StatusServiceUnavailable,
			substr: "lacks SQL permissions",
		},
		{
			name:   "transient outage",
			err:    errors.New("no such repository"),
			want:   http.StatusServiceUnavailable,
			substr: "temporarily unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeUpstreamError(rec, tt.err, "dashboard")

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}

			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if !strings.Contains(resp.Error, tt.substr) {
				t.Fatalf("error = %q, want substring %q", resp.Error, tt.substr)
			}
		})
	}
}
