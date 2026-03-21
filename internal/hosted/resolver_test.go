package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/remote"
	"github.com/gastownhall/wasteland/internal/sdk"
)

type resolverContextKey string

func newFakeNangoForResolver(t *testing.T) *httptest.Server {
	t.Helper()
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{
				Upstream: "wasteland/wl-commons",
				ForkOrg:  "alice-org",
				ForkDB:   "wl-commons",
				Mode:     "wild-west",
			},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer resolver-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method == "GET" && r.URL.Path == "/connection/conn-1" {
			resp := nangoConnectionResponse{
				ConnectionID: "conn-1",
			}
			resp.Credentials.APIKey = "test-dolthub-token"
			b, _ := json.Marshal(meta)
			resp.Metadata = json.RawMessage(b)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
}

func TestWorkspaceResolver_Resolve(t *testing.T) {
	ts := newFakeNangoForResolver(t)
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "resolver-secret",
		IntegrationID: "dolthub",
	})
	sessions := NewSessionStore()
	resolver := NewWorkspaceResolver(nango, sessions)

	session := &UserSession{
		ID:           "sess-1",
		ConnectionID: "conn-1",
		CreatedAt:    time.Now(),
	}

	ws, err := resolver.Resolve(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws == nil {
		t.Fatal("expected non-nil workspace")
	}
	if ws.RigHandle() != "alice" {
		t.Errorf("expected alice, got %s", ws.RigHandle())
	}

	// Should have one upstream.
	upstreams := ws.Upstreams()
	if len(upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(upstreams))
	}
	if upstreams[0].Upstream != "wasteland/wl-commons" {
		t.Errorf("expected wasteland/wl-commons, got %s", upstreams[0].Upstream)
	}

	// Client should be accessible.
	client, err := ws.Client("wasteland/wl-commons")
	if err != nil {
		t.Fatalf("expected client: %v", err)
	}
	if client.RigHandle() != "alice" {
		t.Errorf("expected alice, got %s", client.RigHandle())
	}
	if client.Mode() != "wild-west" {
		t.Errorf("expected wild-west, got %s", client.Mode())
	}
}

func TestWorkspaceResolver_CachesWorkspace(t *testing.T) {
	ts := newFakeNangoForResolver(t)
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "resolver-secret",
		IntegrationID: "dolthub",
	})
	sessions := NewSessionStore()
	resolver := NewWorkspaceResolver(nango, sessions)

	session := &UserSession{
		ID:           "sess-1",
		ConnectionID: "conn-1",
		CreatedAt:    time.Now(),
	}

	ws1, err := resolver.Resolve(session)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	ws2, err := resolver.Resolve(session)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}

	// Should get the same cached instance.
	if ws1 != ws2 {
		t.Error("expected same workspace instance from cache")
	}
}

func TestWorkspaceResolver_Resolve_CoalescesConcurrentMisses(t *testing.T) {
	var calls atomic.Int32
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{
				Upstream: "hop/wl-commons",
				ForkOrg:  "alice-org",
				ForkDB:   "wl-commons",
				Mode:     "pr",
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/connection/conn-1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		resp := nangoConnectionResponse{ConnectionID: "conn-1"}
		resp.Credentials.APIKey = "token"
		b, _ := json.Marshal(meta)
		resp.Metadata = json.RawMessage(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	resolver := NewWorkspaceResolver(nango, NewSessionStore())
	session := &UserSession{ID: "sess-1", ConnectionID: "conn-1", CreatedAt: time.Now()}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws, err := resolver.ResolveContext(context.Background(), session)
			if err != nil {
				t.Errorf("ResolveContext() error = %v", err)
				return
			}
			if ws == nil {
				t.Error("ResolveContext() returned nil workspace")
			}
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("Nango GetConnection calls = %d, want 1", got)
	}
}

func TestWorkspaceResolver_ResolveContext_RespectsCancellationWhileWaiting(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{
				Upstream: "hop/wl-commons",
				ForkOrg:  "alice-org",
				ForkDB:   "wl-commons",
				Mode:     "pr",
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/connection/conn-1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		resp := nangoConnectionResponse{ConnectionID: "conn-1"}
		resp.Credentials.APIKey = "token"
		b, _ := json.Marshal(meta)
		resp.Metadata = json.RawMessage(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	resolver := NewWorkspaceResolver(nango, NewSessionStore())
	session := &UserSession{ID: "sess-1", ConnectionID: "conn-1", CreatedAt: time.Now()}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = resolver.ResolveContext(context.Background(), session)
	}()

	<-started

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveContext(ctx, session)
		errCh <- err
	}()

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ResolveContext() error = %v, want context.Canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("ResolveContext() did not return after cancellation")
	}

	close(release)
	<-firstDone
}

func TestWorkspaceResolver_ResolveContext_KeepsSharedResolveAliveForActiveWaiters(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{
				Upstream: "hop/wl-commons",
				ForkOrg:  "alice-org",
				ForkDB:   "wl-commons",
				Mode:     "pr",
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/connection/conn-1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		resp := nangoConnectionResponse{ConnectionID: "conn-1"}
		resp.Credentials.APIKey = "token"
		b, _ := json.Marshal(meta)
		resp.Metadata = json.RawMessage(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	resolver := NewWorkspaceResolver(nango, NewSessionStore())
	session := &UserSession{ID: "sess-1", ConnectionID: "conn-1", CreatedAt: time.Now()}

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveContext(leaderCtx, session)
		leaderDone <- err
	}()

	<-started

	waiterDone := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveContext(context.Background(), session)
		waiterDone <- err
	}()

	cancelLeader()
	close(release)

	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader ResolveContext() error = %v, want context.Canceled", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter ResolveContext() error = %v, want nil", err)
	}
}

func TestWorkspaceResolver_ResolveContext_FollowerAfterLeaderCancellationStillSucceeds(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{
				Upstream: "hop/wl-commons",
				ForkOrg:  "alice-org",
				ForkDB:   "wl-commons",
				Mode:     "pr",
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/connection/conn-1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		resp := nangoConnectionResponse{ConnectionID: "conn-1"}
		resp.Credentials.APIKey = "token"
		b, _ := json.Marshal(meta)
		resp.Metadata = json.RawMessage(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	resolver := NewWorkspaceResolver(nango, NewSessionStore())
	session := &UserSession{ID: "sess-1", ConnectionID: "conn-1", CreatedAt: time.Now()}

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveContext(leaderCtx, session)
		leaderDone <- err
	}()

	<-started
	cancelLeader()

	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader ResolveContext() error = %v, want context.Canceled", err)
	}

	waiterDone := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveContext(context.Background(), session)
		waiterDone <- err
	}()

	select {
	case err := <-waiterDone:
		t.Fatalf("waiter returned before shared resolve completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)

	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter ResolveContext() error = %v, want nil", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Nango GetConnection calls = %d, want 1", got)
	}
}

func TestWorkspaceResolver_InvalidateConnection(t *testing.T) {
	ts := newFakeNangoForResolver(t)
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "resolver-secret",
		IntegrationID: "dolthub",
	})
	sessions := NewSessionStore()
	resolver := NewWorkspaceResolver(nango, sessions)

	session := &UserSession{
		ID:           "sess-1",
		ConnectionID: "conn-1",
		CreatedAt:    time.Now(),
	}

	ws1, err := resolver.Resolve(session)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	resolver.InvalidateConnection("conn-1")

	ws2, err := resolver.Resolve(session)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}

	if ws1 == ws2 {
		t.Error("expected different workspace after invalidation")
	}
}

func TestWorkspaceResolver_NoToken_StillWorks(t *testing.T) {
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{
				Upstream: "wasteland/wl-commons",
				ForkOrg:  "alice-org",
				ForkDB:   "wl-commons",
				Mode:     "wild-west",
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := nangoConnectionResponse{ConnectionID: "conn-1"}
		b, _ := json.Marshal(meta)
		resp.Metadata = json.RawMessage(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	sessions := NewSessionStore()
	resolver := NewWorkspaceResolver(nango, sessions)

	session := &UserSession{
		ID:           "sess-1",
		ConnectionID: "conn-1",
		CreatedAt:    time.Now(),
	}

	ws, err := resolver.Resolve(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws == nil {
		t.Fatal("expected non-nil workspace")
	}
}

func TestWorkspaceResolver_NoConfig(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := nangoConnectionResponse{ConnectionID: "conn-1"}
		resp.Credentials.APIKey = "token"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	sessions := NewSessionStore()
	resolver := NewWorkspaceResolver(nango, sessions)

	session := &UserSession{
		ID:           "sess-1",
		ConnectionID: "conn-1",
		CreatedAt:    time.Now(),
	}

	_, err := resolver.Resolve(session)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestWorkspaceResolver_MultipleWastelands(t *testing.T) {
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{Upstream: "hop/wl-commons", ForkOrg: "alice-org", ForkDB: "wl-commons", Mode: "wild-west"},
			{Upstream: "gastownhall/gascity", ForkOrg: "alice-org", ForkDB: "gascity", Mode: "pr"},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := nangoConnectionResponse{ConnectionID: "conn-1"}
		resp.Credentials.APIKey = "token"
		b, _ := json.Marshal(meta)
		resp.Metadata = json.RawMessage(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	sessions := NewSessionStore()
	resolver := NewWorkspaceResolver(nango, sessions)

	session := &UserSession{ID: "sess-1", ConnectionID: "conn-1", CreatedAt: time.Now()}

	ws, err := resolver.Resolve(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	upstreams := ws.Upstreams()
	if len(upstreams) != 2 {
		t.Fatalf("expected 2 upstreams, got %d", len(upstreams))
	}

	// Both clients should be accessible.
	c1, err := ws.Client("hop/wl-commons")
	if err != nil {
		t.Fatalf("expected client for hop/wl-commons: %v", err)
	}
	if c1.Mode() != "wild-west" {
		t.Errorf("expected wild-west, got %s", c1.Mode())
	}

	c2, err := ws.Client("gastownhall/gascity")
	if err != nil {
		t.Fatalf("expected client for gastownhall/gascity: %v", err)
	}
	if c2.Mode() != "pr" {
		t.Errorf("expected pr, got %s", c2.Mode())
	}
}

func TestWorkspaceResolver_BuildClientClosures(t *testing.T) {
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{
				Upstream: "hop/wl-commons",
				ForkOrg:  "alice-org",
				ForkDB:   "wl-commons",
			},
		},
	}
	var savedMeta *UserMetadata

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "token"
			b, _ := json.Marshal(meta)
			resp.Metadata = json.RawMessage(b)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodPatch && r.URL.Path == "/connection/conn-1/metadata":
			if err := json.NewDecoder(r.Body).Decode(&savedMeta); err != nil {
				t.Fatalf("decoding metadata patch: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	nango := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "secret",
		IntegrationID: "dolthub",
	})
	resolver := NewWorkspaceResolver(nango, NewSessionStore())

	ws, err := resolver.Resolve(&UserSession{
		ID:           "sess-1",
		ConnectionID: "conn-1",
		CreatedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	client, err := ws.Client("hop/wl-commons")
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if client.Mode() != "pr" {
		t.Fatalf("Mode() = %q, want default pr", client.Mode())
	}
	if got := client.BranchURL("wl/alice/w-1"); got != "https://www.dolthub.com/repositories/alice-org/wl-commons/data/wl%2Falice%2Fw-1" {
		t.Fatalf("BranchURL() = %q, want encoded DoltHub URL", got)
	}
	if err := client.SaveSettings("wild-west", true); err != nil {
		t.Fatalf("SaveSettings() error = %v", err)
	}
	if savedMeta == nil {
		t.Fatal("expected metadata patch")
	}
	entry := savedMeta.FindWasteland("hop/wl-commons")
	if entry == nil || entry.Mode != "wild-west" || !entry.Signing {
		t.Fatalf("saved metadata = %+v", savedMeta)
	}
	if err := client.CloseUpstreamPR("https://example.com/no-pr-here"); err == nil {
		t.Fatal("expected invalid PR URL error")
	}
	if _, _, _, err := client.LoadPendingDetail("w-1", sdk.PendingItem{}); err == nil {
		t.Fatal("expected missing fork owner/branch error")
	}
}

func TestExtractWantedIDFromBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		branch string
		want   string
	}{
		{branch: "wl/alice/w-123", want: "w-123"},
		{branch: "wl/alice/nested/id", want: "nested/id"},
		{branch: "feature/alice/w-123", want: "feature/alice/w-123"},
	}

	for _, tt := range tests {
		if got := extractWantedIDFromBranch(tt.branch); got != tt.want {
			t.Fatalf("extractWantedIDFromBranch(%q) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

func TestExtractPRID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url  string
		want string
	}{
		{
			url:  "https://www.dolthub.com/repositories/org/db/pulls/123",
			want: "123",
		},
		{
			url:  "https://www.dolthub.com/repositories/org/db/pulls/123?tab=files",
			want: "123?tab=files",
		},
		{
			url:  "https://www.dolthub.com/repositories/org/db",
			want: "",
		},
	}

	for _, tt := range tests {
		if got := extractPRID(tt.url); got != tt.want {
			t.Fatalf("extractPRID(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestPendingUpstreamCache_Get(t *testing.T) {
	t.Parallel()

	cache := &pendingUpstreamCache{
		cached: map[string][]sdk.PendingItem{
			"w-1": {{RigHandle: "alice", Status: "claimed"}},
		},
	}
	got, err := cache.Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got["w-1"]) != 1 || got["w-1"][0].RigHandle != "alice" {
		t.Fatalf("Get() = %+v", got)
	}
}

type pendingCacheResult struct {
	items map[string][]sdk.PendingItem
	err   error
}

func TestPendingUpstreamCache_GetContext_StaleCachedRefreshesBeforeReturningAndUsesRequestContext(t *testing.T) {
	t.Parallel()

	key := resolverContextKey("pending-stale-refresh")
	ctxSeen := make(chan any, 1)
	release := make(chan struct{})
	var calls atomic.Int32

	provider := remote.NewDoltHubProviderWithClient(&http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			select {
			case ctxSeen <- req.Context().Value(key):
			default:
			}
			switch req.URL.Path {
			case "/api/v1alpha1/wasteland/wl-commons/pulls":
				return jsonResponse(`{
					"pulls":[
						{"pull_id":"1","state":"open"}
					]
				}`), nil
			case "/api/v1alpha1/wasteland/wl-commons/pulls/1":
				return jsonResponse(`{
					"from_branch":"wl/alice/w-1",
					"from_branch_owner":"alice",
					"author":"alice"
				}`), nil
			case "/api/v1alpha1/alice/wl-commons/wl/alice/w-1":
				calls.Add(1)
				<-release
				return jsonResponse(`{
					"rows":[
						{"id":"w-1","status":"claimed","claimed_by":"alice","diff_type":"modified"}
					]
				}`), nil
			default:
				t.Fatalf("unexpected request path: %s", req.URL.Path)
				return nil, nil
			}
		}),
	})

	cache := &pendingUpstreamCache{
		cached: map[string][]sdk.PendingItem{
			"w-1": {{RigHandle: "stale", Status: "claimed"}},
		},
		refreshedAt: time.Now().Add(-2 * time.Minute),
		provider:    provider,
		upOrg:       "wasteland",
		upDB:        "wl-commons",
		interval:    time.Minute,
		stop:        make(chan struct{}),
	}
	defer cache.Stop()

	resultCh := make(chan pendingCacheResult, 1)
	go func() {
		items, err := cache.GetContext(context.WithValue(context.Background(), key, "trace-bound"))
		resultCh <- pendingCacheResult{items: items, err: err}
	}()

	for i := 0; i < 50 && calls.Load() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}
	select {
	case got := <-ctxSeen:
		if got != "trace-bound" {
			t.Fatalf("refresh context value = %v, want trace-bound", got)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("stale refresh did not observe request context")
	}
	select {
	case got := <-resultCh:
		t.Fatalf("GetContext() returned early with %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	got := <-resultCh
	if got.err != nil {
		t.Fatalf("GetContext() error = %v", got.err)
	}
	if len(got.items["w-1"]) != 1 || got.items["w-1"][0].RigHandle != "alice" {
		t.Fatalf("GetContext() = %+v, want refreshed alice entry", got.items)
	}
}

func TestPendingUpstreamCache_GetContext_CanceledStaleCallerKeepsSharedRefreshAlive(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var calls atomic.Int32

	provider := remote.NewDoltHubProviderWithClient(&http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/api/v1alpha1/wasteland/wl-commons/pulls":
				return jsonResponse(`{
					"pulls":[
						{"pull_id":"1","state":"open"}
					]
				}`), nil
			case "/api/v1alpha1/wasteland/wl-commons/pulls/1":
				return jsonResponse(`{
					"from_branch":"wl/alice/w-1",
					"from_branch_owner":"alice",
					"author":"alice"
				}`), nil
			case "/api/v1alpha1/alice/wl-commons/wl/alice/w-1":
				calls.Add(1)
				<-release
				return jsonResponse(`{
					"rows":[
						{"id":"w-1","status":"claimed","claimed_by":"alice","diff_type":"modified"}
					]
				}`), nil
			default:
				t.Fatalf("unexpected request path: %s", req.URL.Path)
				return nil, nil
			}
		}),
	})

	cache := &pendingUpstreamCache{
		cached: map[string][]sdk.PendingItem{
			"w-1": {{RigHandle: "stale", Status: "claimed"}},
		},
		refreshedAt: time.Now().Add(-2 * time.Minute),
		provider:    provider,
		upOrg:       "wasteland",
		upDB:        "wl-commons",
		interval:    time.Minute,
		stop:        make(chan struct{}),
	}
	defer cache.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	firstErrCh := make(chan error, 1)
	go func() {
		_, err := cache.GetContext(ctx)
		firstErrCh <- err
	}()

	for i := 0; i < 50 && calls.Load() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}

	cancel()
	select {
	case err := <-firstErrCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first GetContext() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled GetContext() did not return")
	}

	secondDone := make(chan pendingCacheResult, 1)
	go func() {
		items, err := cache.GetContext(context.Background())
		secondDone <- pendingCacheResult{items: items, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("refresh calls after second GetContext() = %d, want 1", calls.Load())
	}
	select {
	case got := <-secondDone:
		t.Fatalf("second GetContext() returned before shared refresh completed: %+v", got)
	default:
	}

	close(release)
	got := <-secondDone
	if got.err != nil {
		t.Fatalf("second GetContext() error = %v", got.err)
	}
	if len(got.items["w-1"]) != 1 || got.items["w-1"][0].RigHandle != "alice" {
		t.Fatalf("second GetContext() = %+v, want refreshed alice entry", got.items)
	}
}

func TestNewPendingUpstreamCache_WarmupIsAsyncAndCoalescesFirstReaders(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var calls atomic.Int32

	provider := remote.NewDoltHubProviderWithClient(&http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/api/v1alpha1/wasteland/wl-commons/pulls":
				return jsonResponse(`{
					"pulls":[
						{"pull_id":"1","state":"open"}
					]
				}`), nil
			case "/api/v1alpha1/wasteland/wl-commons/pulls/1":
				return jsonResponse(`{
					"from_branch":"wl/alice/w-1",
					"from_branch_owner":"alice",
					"author":"alice"
				}`), nil
			case "/api/v1alpha1/alice/wl-commons/wl/alice/w-1":
				calls.Add(1)
				<-release
				return jsonResponse(`{
					"rows":[
						{"id":"w-1","status":"claimed","claimed_by":"alice","diff_type":"modified"}
					]
				}`), nil
			default:
				t.Fatalf("unexpected request path: %s", req.URL.Path)
				return nil, nil
			}
		}),
	})

	created := make(chan *pendingUpstreamCache, 1)
	go func() {
		created <- newPendingUpstreamCache(provider, "wasteland", "wl-commons", time.Minute)
	}()

	var cache *pendingUpstreamCache
	select {
	case cache = <-created:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("newPendingUpstreamCache blocked on initial warmup")
	}
	defer cache.Stop()

	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := cache.GetContext(context.Background())
			errCh <- err
		}()
	}

	for i := 0; i < 50 && calls.Load() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}

	time.Sleep(25 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("refresh calls before release = %d, want 1", calls.Load())
	}

	close(release)
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("GetContext() error = %v", err)
		}
	}
}

func TestPendingUpstreamCache_StopWaitsForInFlightRefresh(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var calls atomic.Int32

	provider := remote.NewDoltHubProviderWithClient(&http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/api/v1alpha1/wasteland/wl-commons/pulls":
				return jsonResponse(`{
					"pulls":[
						{"pull_id":"1","state":"open"}
					]
				}`), nil
			case "/api/v1alpha1/wasteland/wl-commons/pulls/1":
				return jsonResponse(`{
					"from_branch":"wl/alice/w-1",
					"from_branch_owner":"alice",
					"author":"alice"
				}`), nil
			case "/api/v1alpha1/alice/wl-commons/wl/alice/w-1":
				calls.Add(1)
				<-release
				return jsonResponse(`{
					"rows":[
						{"id":"w-1","status":"claimed","claimed_by":"alice","diff_type":"modified"}
					]
				}`), nil
			default:
				t.Fatalf("unexpected request path: %s", req.URL.Path)
				return nil, nil
			}
		}),
	})

	cache := &pendingUpstreamCache{
		cached: map[string][]sdk.PendingItem{
			"w-1": {{RigHandle: "stale", Status: "claimed"}},
		},
		refreshedAt: time.Now().Add(-2 * time.Minute),
		provider:    provider,
		upOrg:       "wasteland",
		upDB:        "wl-commons",
		interval:    time.Minute,
		stop:        make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := cache.GetContext(context.Background())
		errCh <- err
	}()

	for i := 0; i < 50 && calls.Load() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}

	stopped := make(chan struct{})
	go func() {
		cache.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop() returned before in-flight refresh completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not wait for in-flight refresh")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
