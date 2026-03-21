package api

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/gastownhall/wasteland/internal/ctxutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// ReadCache is a keyed read-through cache with TTL and thundering-herd
// protection. It serves pre-serialized JSON bytes so that multiple HTTP
// readers can share a single []byte without re-marshaling.
type ReadCache struct {
	mu         sync.Mutex
	entries    map[string]*cacheEntry
	inflight   map[string]*call
	maxAge     time.Duration
	maxEntries int
}

type cacheEntry struct {
	data     []byte
	storedAt time.Time
}

var readCacheTracer = otel.Tracer("github.com/gastownhall/wasteland/internal/api/read_cache")

const readCacheFetchTimeout = 30 * time.Second

// call represents an in-flight fetch. Multiple concurrent callers for the
// same key share a single fetch and wait independently on its completion.
type call struct {
	done chan struct{}
	val  []byte
	err  error
}

// NewReadCache creates a cache that keeps entries for maxAge and evicts the
// oldest when maxEntries is exceeded.
func NewReadCache(maxAge time.Duration, maxEntries int) *ReadCache {
	return &ReadCache{
		entries:    make(map[string]*cacheEntry),
		inflight:   make(map[string]*call),
		maxAge:     maxAge,
		maxEntries: maxEntries,
	}
}

// Get returns cached bytes if fresh, nil if miss or stale.
func (c *ReadCache) Get(key string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	if time.Since(e.storedAt) > c.maxAge {
		return nil
	}
	return e.data
}

// GetStale returns cached bytes even if expired, for fallback during outages.
// Returns nil only if the key was never cached.
func (c *ReadCache) GetStale(key string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	return e.data
}

// GetOrFetch returns cached bytes for key, or calls fn to fetch them.
// Concurrent callers for the same key are coalesced: only the first caller
// runs fn while the rest block on its result (singleflight pattern).
func (c *ReadCache) GetOrFetch(key string, fn func() ([]byte, error)) ([]byte, error) {
	return c.GetOrFetchContext(context.Background(), key, func(context.Context) ([]byte, error) {
		return fn()
	})
}

// GetOrFetchContext is GetOrFetch with context-aware tracing for cache misses.
func (c *ReadCache) GetOrFetchContext(ctx context.Context, key string, fn func(context.Context) ([]byte, error)) ([]byte, error) {
	// Fast path: cache hit.
	if data := c.Get(key); data != nil {
		trace.SpanFromContext(ctx).AddEvent("read_cache.hit")
		return data, nil
	}

	c.mu.Lock()
	// Double-check after acquiring lock.
	if e, ok := c.entries[key]; ok && time.Since(e.storedAt) <= c.maxAge {
		c.mu.Unlock()
		trace.SpanFromContext(ctx).AddEvent("read_cache.hit")
		return e.data, nil
	}

	// Join an in-flight fetch if one exists.
	if cl, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		trace.SpanFromContext(ctx).AddEvent("read_cache.wait_inflight")
		return waitForCall(ctx, cl)
	}

	// First caller: create a new in-flight entry.
	cl := &call{done: make(chan struct{})}
	c.inflight[key] = cl
	c.mu.Unlock()

	go func() {
		fetchCtx, cancel := ctxutil.Detached(ctx, readCacheFetchTimeout)
		defer cancel()
		c.runFetch(fetchCtx, key, cl, fn)
	}()
	return waitForCall(ctx, cl)
}

// Invalidate clears all cached entries.
func (c *ReadCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}

// InvalidateKey removes a single cached entry.
func (c *ReadCache) InvalidateKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *ReadCache) runFetch(ctx context.Context, key string, cl *call, fn func(context.Context) ([]byte, error)) {
	defer close(cl.done)

	fetchCtx, span := readCacheTracer.Start(ctx, "read_cache.fetch")
	cl.val, cl.err = fn(fetchCtx)
	if cl.err != nil {
		span.RecordError(cl.err)
	}
	span.End()

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, key)
	if cl.err == nil {
		c.entries[key] = &cacheEntry{data: cl.val, storedAt: time.Now()}
		c.evictLocked()
	}
}

func waitForCall(ctx context.Context, cl *call) ([]byte, error) {
	select {
	case <-cl.done:
		return cl.val, cl.err
	case <-ctx.Done():
		select {
		case <-cl.done:
			return cl.val, cl.err
		default:
		}
		return nil, ctx.Err()
	}
}

// evictLocked removes the oldest entries when the cache exceeds maxEntries.
// Must be called with c.mu held.
func (c *ReadCache) evictLocked() {
	if len(c.entries) <= c.maxEntries {
		return
	}

	type kv struct {
		key      string
		storedAt time.Time
	}
	items := make([]kv, 0, len(c.entries))
	for k, e := range c.entries {
		items = append(items, kv{key: k, storedAt: e.storedAt})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].storedAt.Before(items[j].storedAt)
	})

	excess := len(c.entries) - c.maxEntries
	for i := 0; i < excess; i++ {
		delete(c.entries, items[i].key)
	}
}
