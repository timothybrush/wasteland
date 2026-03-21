package hosted

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gastownhall/wasteland/internal/backend"
	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/ctxutil"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
	"github.com/gastownhall/wasteland/internal/sdk"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

const (
	cacheTTL              = 1 * time.Minute
	resolveMissTimeout    = 30 * time.Second
	pendingRefreshTimeout = 30 * time.Second
)

var hostedTracer = otel.Tracer("github.com/gastownhall/wasteland/internal/hosted")

type cachedWorkspace struct {
	workspace *sdk.Workspace
	expiresAt time.Time
}

// WorkspaceResolver resolves per-user sdk.Workspaces from Nango credentials.
type WorkspaceResolver struct {
	nango    *NangoClient
	sessions *SessionStore
	mu       sync.Mutex
	cache    map[string]*cachedWorkspace // connectionID -> cached workspace
	group    singleflight.Group

	pendingMu    sync.Mutex
	pendingCache map[string]*pendingUpstreamCache // upstream ("org/db") -> shared cache
}

// pendingUpstreamCache is a shared pending-items cache that refreshes in the
// background and can synchronously refresh on cold/stale request reads.
type pendingUpstreamCache struct {
	mu          sync.RWMutex
	cached      map[string][]sdk.PendingItem
	refreshedAt time.Time
	provider    *remote.DoltHubProvider
	upOrg       string
	upDB        string
	interval    time.Duration
	inflight    *pendingRefresh
	stop        chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

type pendingRefresh struct {
	done  chan struct{}
	items map[string][]sdk.PendingItem
	err   error
}

func newPendingUpstreamCache(provider *remote.DoltHubProvider, upOrg, upDB string, interval time.Duration) *pendingUpstreamCache {
	c := &pendingUpstreamCache{
		provider: provider,
		upOrg:    upOrg,
		upDB:     upDB,
		interval: interval,
		stop:     make(chan struct{}),
	}

	// Warm in the background so cache creation stays cheap; first readers coalesce
	// on the same shared refresh if the warmup is still in flight.
	c.scheduleRefresh(context.Background())

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.scheduleRefresh(context.Background())
			case <-c.stop:
				return
			}
		}
	}()

	return c
}

func (c *pendingUpstreamCache) Stop() {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		close(c.stop)
		c.mu.Unlock()
	})
	c.wg.Wait()
}

func (c *pendingUpstreamCache) Get() (map[string][]sdk.PendingItem, error) {
	c.mu.RLock()
	cached := c.cached
	stale := c.provider != nil && cached != nil && time.Since(c.refreshedAt) >= c.interval
	refreshing := c.inflight != nil
	c.mu.RUnlock()
	if stale && !refreshing {
		c.scheduleRefresh(context.Background())
	}
	return cached, nil
}

func (c *pendingUpstreamCache) GetContext(ctx context.Context) (map[string][]sdk.PendingItem, error) {
	c.mu.RLock()
	cached := c.cached
	stale := c.provider != nil && (cached == nil || time.Since(c.refreshedAt) >= c.interval)
	c.mu.RUnlock()
	if !stale {
		return cached, nil
	}
	// Request-scoped reads must not silently serve stale pending metadata. The
	// outer API cache can decide whether to serve a stale full response on error.
	return c.refreshSync(ctx)
}

func (c *pendingUpstreamCache) refreshSync(ctx context.Context) (map[string][]sdk.PendingItem, error) {
	refresh := c.startRefresh(ctx)
	select {
	case <-refresh.done:
		if refresh.err != nil {
			return nil, refresh.err
		}
		return refresh.items, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *pendingUpstreamCache) scheduleRefresh(ctx context.Context) {
	refresh := c.startRefresh(ctx)
	go func() {
		<-refresh.done
		if refresh.err != nil {
			slog.Warn("pending items refresh failed", "upstream", c.upOrg+"/"+c.upDB, "error", refresh.err)
		}
	}()
}

func (c *pendingUpstreamCache) refreshNow(ctx context.Context) (map[string][]sdk.PendingItem, error) {
	if c.provider == nil {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.cached, nil
	}
	states, err := c.provider.WithContext(ctx).ListPendingWantedIDs(c.upOrg, c.upDB)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]sdk.PendingItem, len(states))
	for id, pending := range states {
		items := make([]sdk.PendingItem, len(pending))
		for i, p := range pending {
			items[i] = sdk.PendingItem{
				RigHandle:   p.RigHandle,
				Status:      p.Status,
				ClaimedBy:   p.ClaimedBy,
				Branch:      p.Branch,
				BranchURL:   p.BranchURL,
				PRURL:       p.PRURL,
				ForkOwner:   p.ForkOwner,
				CompletedBy: p.CompletedBy,
				Evidence:    p.Evidence,
			}
		}
		result[id] = items
	}

	c.mu.Lock()
	c.cached = result
	c.refreshedAt = time.Now()
	c.mu.Unlock()
	return result, nil
}

func (c *pendingUpstreamCache) startRefresh(ctx context.Context) *pendingRefresh {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.provider == nil {
		refresh := &pendingRefresh{
			done:  make(chan struct{}),
			items: c.cached,
		}
		close(refresh.done)
		return refresh
	}
	if c.inflight != nil {
		return c.inflight
	}
	select {
	case <-c.stop:
		refresh := &pendingRefresh{
			done:  make(chan struct{}),
			items: c.cached,
		}
		close(refresh.done)
		return refresh
	default:
	}

	refresh := &pendingRefresh{done: make(chan struct{})}
	c.inflight = refresh
	c.wg.Add(1)
	go c.runRefresh(ctx, refresh)
	return refresh
}

func (c *pendingUpstreamCache) runRefresh(ctx context.Context, refresh *pendingRefresh) {
	defer c.wg.Done()

	sharedCtx, cancel := ctxutil.Detached(ctx, pendingRefreshTimeout)
	defer cancel()

	items, err := c.refreshNow(sharedCtx)

	c.mu.Lock()
	if c.inflight == refresh {
		c.inflight = nil
	}
	c.mu.Unlock()

	refresh.items = items
	refresh.err = err
	close(refresh.done)
}

// NewWorkspaceResolver creates a WorkspaceResolver.
func NewWorkspaceResolver(nango *NangoClient, sessions *SessionStore) *WorkspaceResolver {
	return &WorkspaceResolver{
		nango:        nango,
		sessions:     sessions,
		cache:        make(map[string]*cachedWorkspace),
		pendingCache: make(map[string]*pendingUpstreamCache),
	}
}

// Stop shuts down any shared pending caches owned by the resolver.
func (wr *WorkspaceResolver) Stop() {
	wr.pendingMu.Lock()
	caches := make([]*pendingUpstreamCache, 0, len(wr.pendingCache))
	for _, cache := range wr.pendingCache {
		caches = append(caches, cache)
	}
	wr.pendingMu.Unlock()

	for _, cache := range caches {
		cache.Stop()
	}
}

// Resolve builds or returns a cached sdk.Workspace for the given session.
func (wr *WorkspaceResolver) Resolve(session *UserSession) (*sdk.Workspace, error) {
	return wr.ResolveContext(context.Background(), session)
}

// ResolveContext builds or returns a cached sdk.Workspace for the given session.
func (wr *WorkspaceResolver) ResolveContext(ctx context.Context, session *UserSession) (*sdk.Workspace, error) {
	ctx, span := hostedTracer.Start(ctx, "hosted.workspace.resolve")
	defer span.End()

	// Fast path: return cached workspace if still valid.
	if cached, ok := wr.cachedWorkspace(session.ConnectionID); ok {
		span.SetAttributes(attribute.Bool("cache.hit", true))
		return cached, nil
	}
	span.SetAttributes(attribute.Bool("cache.hit", false))

	resultCh := wr.group.DoChan(session.ConnectionID, func() (any, error) {
		resolveCtx, cancel := ctxutil.Detached(ctx, resolveMissTimeout)
		defer cancel()
		return wr.resolveMiss(resolveCtx, session)
	})
	return wr.waitOnResolveResult(ctx, span, resultCh)
}

// WarmSession primes the workspace cache using metadata already fetched on the
// boot path. It shares the same singleflight key as live resolves.
func (wr *WorkspaceResolver) WarmSession(session *UserSession, apiKey string, meta *UserMetadata) {
	if session == nil || session.ConnectionID == "" || meta == nil || len(meta.Wastelands) == 0 {
		return
	}
	go func() {
		ctx, cancel := ctxutil.Detached(context.Background(), resolveMissTimeout)
		defer cancel()
		if _, ok := wr.cachedWorkspace(session.ConnectionID); ok {
			return
		}
		resultCh := wr.group.DoChan(session.ConnectionID, func() (any, error) {
			return wr.resolveFromMetadata(ctx, session.ConnectionID, apiKey, meta)
		})
		select {
		case <-resultCh:
		case <-ctx.Done():
		}
	}()
}

func (wr *WorkspaceResolver) waitOnResolveResult(ctx context.Context, span trace.Span, resultCh <-chan singleflight.Result) (*sdk.Workspace, error) {
	_, waitSpan := hostedTracer.Start(ctx, "hosted.workspace.wait_for_resolve")
	defer waitSpan.End()

	select {
	case result := <-resultCh:
		waitSpan.SetAttributes(attribute.Bool("singleflight.shared", result.Shared))
		span.SetAttributes(attribute.Bool("singleflight.shared", result.Shared))
		if result.Err != nil {
			waitSpan.RecordError(result.Err)
			span.RecordError(result.Err)
			return nil, result.Err
		}
		workspace, _ := result.Val.(*sdk.Workspace)
		return workspace, nil
	case <-ctx.Done():
		waitSpan.RecordError(ctx.Err())
		span.RecordError(ctx.Err())
		return nil, ctx.Err()
	}
}

func (wr *WorkspaceResolver) resolveMiss(ctx context.Context, session *UserSession) (*sdk.Workspace, error) {
	resolveCtx, resolveSpan := hostedTracer.Start(ctx, "hosted.workspace.resolve_miss")
	defer resolveSpan.End()

	// Re-check cache inside the winner path in case another request warmed it.
	if cached, ok := wr.cachedWorkspace(session.ConnectionID); ok {
		resolveSpan.SetAttributes(attribute.Bool("cache.hit", true))
		return cached, nil
	}
	resolveSpan.SetAttributes(attribute.Bool("cache.hit", false))

	apiKey, meta, err := wr.nango.GetConnectionContext(resolveCtx, session.ConnectionID)
	if err != nil {
		resolveSpan.RecordError(err)
		return nil, fmt.Errorf("resolving credentials: %w", err)
	}
	workspace, err := wr.resolveFromMetadata(resolveCtx, session.ConnectionID, apiKey, meta)
	if err != nil {
		resolveSpan.RecordError(err)
		return nil, err
	}
	resolveSpan.SetAttributes(attribute.Int("wasteland.count", len(meta.Wastelands)))
	return workspace, nil
}

func (wr *WorkspaceResolver) resolveFromMetadata(_ context.Context, connectionID, apiKey string, meta *UserMetadata) (*sdk.Workspace, error) {
	if meta == nil || len(meta.Wastelands) == 0 {
		return nil, fmt.Errorf("no wasteland config found for connection %s", connectionID)
	}

	ws := sdk.NewWorkspace(meta.RigHandle)
	for i := range meta.Wastelands {
		wl := &meta.Wastelands[i]
		client, err := wr.buildClient(wl, meta.RigHandle, connectionID, apiKey, meta)
		if err != nil {
			return nil, fmt.Errorf("building client for %s: %w", wl.Upstream, err)
		}
		ws.Add(sdk.UpstreamInfo{
			Upstream: wl.Upstream,
			ForkOrg:  wl.ForkOrg,
			ForkDB:   wl.ForkDB,
			Mode:     wl.Mode,
		}, client)
	}

	wr.cacheWorkspace(connectionID, ws)
	return ws, nil
}

// InvalidateConnection removes the cached workspace for a connection.
func (wr *WorkspaceResolver) InvalidateConnection(connectionID string) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	delete(wr.cache, connectionID)
}

func (wr *WorkspaceResolver) cachedWorkspace(connectionID string) (*sdk.Workspace, bool) {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	cached, ok := wr.cache[connectionID]
	if !ok || time.Now().After(cached.expiresAt) {
		if ok {
			delete(wr.cache, connectionID)
		}
		return nil, false
	}
	return cached.workspace, true
}

func (wr *WorkspaceResolver) cacheWorkspace(connectionID string, ws *sdk.Workspace) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.cache[connectionID] = &cachedWorkspace{
		workspace: ws,
		expiresAt: time.Now().Add(cacheTTL),
	}
}

// getOrCreatePendingCache returns a shared background-refreshing cache for the
// given upstream. All users on the same upstream share a single cache instance.
func (wr *WorkspaceResolver) getOrCreatePendingCache(provider *remote.DoltHubProvider, upOrg, upDB string) *pendingUpstreamCache {
	key := upOrg + "/" + upDB
	wr.pendingMu.Lock()
	defer wr.pendingMu.Unlock()
	if c, ok := wr.pendingCache[key]; ok {
		return c
	}
	c := newPendingUpstreamCache(provider, upOrg, upDB, 2*time.Minute)
	wr.pendingCache[key] = c
	return c
}

func (wr *WorkspaceResolver) buildClient(wl *WastelandConfig, rigHandle, connectionID, apiKey string, _ *UserMetadata) (*sdk.Client, error) {
	upOrg, upDB, err := federation.ParseUpstream(wl.Upstream)
	if err != nil {
		return nil, fmt.Errorf("parsing upstream %q: %w", wl.Upstream, err)
	}

	mode := wl.Mode
	if mode == "" {
		mode = "pr"
	}

	db := backend.NewRemoteDB(apiKey, upOrg, upDB, wl.ForkOrg, wl.ForkDB, mode)

	provider := remote.NewDoltHubProvider(apiKey)

	branchURL := func(branch string) string {
		return fmt.Sprintf("https://www.dolthub.com/repositories/%s/%s/data/%s",
			wl.ForkOrg, wl.ForkDB, strings.ReplaceAll(branch, "/", "%2F"))
	}

	// Capture the upstream for the SaveConfig closure.
	upstream := wl.Upstream

	client := sdk.New(sdk.ClientConfig{
		DB:                     db,
		RigHandle:              rigHandle,
		Upstream:               wl.Upstream,
		Mode:                   mode,
		BestEffortPendingReads: true,
		LoadDiff: func(branch string) (string, error) {
			return db.Diff(branch)
		},
		CreatePR: func(branch string) (string, error) {
			// Build PR title from the wanted item.
			wantedID := extractWantedIDFromBranch(branch)
			prTitle := fmt.Sprintf("[wl] %s", wantedID)
			if item, _, _, qerr := commons.QueryFullDetailAsOf(db, wantedID, branch); qerr == nil && item != nil {
				prTitle = fmt.Sprintf("[wl] %s", item.Title)
			}

			// Build PR description from the branch diff.
			var prBody string
			if diff, derr := db.Diff(branch); derr == nil {
				prBody = diff
			}

			prURL, err := provider.CreatePR(wl.ForkOrg, upOrg, upDB, branch, prTitle, prBody)
			if err != nil && strings.Contains(err.Error(), "already exists") {
				existingURL, existingID := provider.FindPR(upOrg, upDB, wl.ForkOrg, branch)
				if existingID != "" {
					if uerr := provider.UpdatePR(upOrg, upDB, existingID, prTitle, prBody); uerr != nil {
						slog.Warn("failed to update existing PR", "pr_id", existingID, "error", uerr)
					}
					return existingURL, nil
				}
			}
			return prURL, err
		},
		CheckPR: func(branch string) string {
			url, _ := provider.FindPR(upOrg, upDB, wl.ForkOrg, branch)
			return url
		},
		CheckPRContext: func(ctx context.Context, branch string) string {
			if provider == nil {
				return ""
			}
			url, _ := provider.WithContext(ctx).FindPR(upOrg, upDB, wl.ForkOrg, branch)
			return url
		},
		ClosePR: func(branch string) error {
			_, prID := provider.FindPR(upOrg, upDB, wl.ForkOrg, branch)
			if prID == "" {
				return nil
			}
			return provider.ClosePR(upOrg, upDB, prID)
		},
		CloseUpstreamPR: func(prURL string) error {
			prID := extractPRID(prURL)
			if prID == "" {
				return fmt.Errorf("cannot extract PR ID from URL: %s", prURL)
			}
			return provider.ClosePR(upOrg, upDB, prID)
		},
		ListPendingItems: wr.getOrCreatePendingCache(provider, upOrg, upDB).Get,
		ListPendingItemsContext: func(ctx context.Context) (map[string][]sdk.PendingItem, error) {
			return wr.getOrCreatePendingCache(provider, upOrg, upDB).GetContext(ctx)
		},
		BranchURL: branchURL,
		Signing:   wl.Signing,
		SaveConfig: func(mode string, signing bool) error {
			// Read-modify-write: fetch current metadata, update just this wasteland, write back.
			_, currentMeta, err := wr.nango.GetConnection(connectionID)
			if err != nil {
				return fmt.Errorf("reading metadata for save: %w", err)
			}
			if currentMeta == nil {
				return fmt.Errorf("no metadata found for connection %s", connectionID)
			}
			entry := currentMeta.FindWasteland(upstream)
			if entry == nil {
				return fmt.Errorf("wasteland %s not found in metadata", upstream)
			}
			entry.Mode = mode
			entry.Signing = signing
			return wr.nango.SetMetadata(connectionID, currentMeta)
		},
		LoadPendingItem: func(wantedID string, pending sdk.PendingItem) (*commons.WantedItem, error) {
			if pending.ForkOwner == "" || pending.Branch == "" {
				return nil, fmt.Errorf("pending item %q is missing fork owner or branch", wantedID)
			}
			forkDB := backend.NewRemoteDB(apiKey, upOrg, upDB, pending.ForkOwner, upDB, mode)
			return commons.QueryWantedDetailAsOf(forkDB, wantedID, pending.Branch)
		},
		LoadPendingItemContext: func(ctx context.Context, wantedID string, pending sdk.PendingItem) (*commons.WantedItem, error) {
			if pending.ForkOwner == "" || pending.Branch == "" {
				return nil, fmt.Errorf("pending item %q is missing fork owner or branch", wantedID)
			}
			forkDB := backend.NewRemoteDB(apiKey, upOrg, upDB, pending.ForkOwner, upDB, mode)
			return commons.QueryWantedDetailAsOf(forkDB.WithContext(ctx), wantedID, pending.Branch)
		},
		LoadPendingDetail: func(wantedID string, pending sdk.PendingItem) (*commons.WantedItem, *commons.CompletionRecord, *commons.Stamp, error) {
			if pending.ForkOwner == "" || pending.Branch == "" {
				return nil, nil, nil, fmt.Errorf("pending item %q is missing fork owner or branch", wantedID)
			}
			forkDB := backend.NewRemoteDB(apiKey, upOrg, upDB, pending.ForkOwner, upDB, mode)
			return commons.QueryFullDetailAsOf(forkDB, wantedID, pending.Branch)
		},
		LoadPendingDetailContext: func(ctx context.Context, wantedID string, pending sdk.PendingItem) (*commons.WantedItem, *commons.CompletionRecord, *commons.Stamp, error) {
			if pending.ForkOwner == "" || pending.Branch == "" {
				return nil, nil, nil, fmt.Errorf("pending item %q is missing fork owner or branch", wantedID)
			}
			forkDB := backend.NewRemoteDB(apiKey, upOrg, upDB, pending.ForkOwner, upDB, mode)
			return commons.QueryFullDetailAsOf(forkDB.WithContext(ctx), wantedID, pending.Branch)
		},
	})

	return client, nil
}

// extractWantedIDFromBranch parses a branch name like "wl/{rig}/{wantedID}"
// and returns the wanted ID, or the raw branch name as fallback.
func extractWantedIDFromBranch(branch string) string {
	parts := strings.SplitN(branch, "/", 3)
	if len(parts) == 3 && parts[0] == "wl" {
		return parts[2]
	}
	return branch
}

// extractPRID extracts the pull request ID from a DoltHub PR URL like
// "https://www.dolthub.com/repositories/org/db/pulls/123".
func extractPRID(prURL string) string {
	idx := strings.LastIndex(prURL, "/pulls/")
	if idx < 0 {
		return ""
	}
	return prURL[idx+len("/pulls/"):]
}
