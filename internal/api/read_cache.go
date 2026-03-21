package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
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
	generation map[string]uint64
	maxAge     time.Duration
	maxEntries int
}

type cacheEntry struct {
	data     []byte
	storedAt time.Time
}

var readCacheTracer = otel.Tracer("github.com/gastownhall/wasteland/internal/api/read_cache")

const readCacheFetchTimeout = 30 * time.Second

var errReadCacheInvalidated = errors.New("read cache invalidated during fetch")

// call represents an in-flight fetch. Multiple concurrent callers for the
// same key share a single fetch and wait independently on its completion.
type call struct {
	done        chan struct{}
	val         []byte
	err         error
	generation  uint64
	invalidated atomic.Bool
}

// NewReadCache creates a cache that keeps entries for maxAge and evicts the
// oldest when maxEntries is exceeded.
func NewReadCache(maxAge time.Duration, maxEntries int) *ReadCache {
	return &ReadCache{
		entries:    make(map[string]*cacheEntry),
		inflight:   make(map[string]*call),
		generation: make(map[string]uint64),
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
	for {
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
			data, err := waitForCall(ctx, cl)
			if errors.Is(err, errReadCacheInvalidated) {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				trace.SpanFromContext(ctx).AddEvent("read_cache.retry_invalidated")
				continue
			}
			return data, err
		}

		// First caller: create a new in-flight entry.
		cl := &call{
			done:       make(chan struct{}),
			generation: c.generation[key],
		}
		c.inflight[key] = cl
		c.mu.Unlock()

		go func() {
			fetchCtx, cancel := ctxutil.Detached(ctx, readCacheFetchTimeout)
			defer cancel()
			c.runFetch(fetchCtx, key, cl, fn)
		}()
		data, err := waitForCall(ctx, cl)
		if errors.Is(err, errReadCacheInvalidated) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			trace.SpanFromContext(ctx).AddEvent("read_cache.retry_invalidated")
			continue
		}
		return data, err
	}
}

// Invalidate clears all cached entries.
func (c *ReadCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := make(map[string]struct{}, len(c.entries)+len(c.inflight))
	for key := range c.entries {
		seen[key] = struct{}{}
	}
	for key := range c.inflight {
		seen[key] = struct{}{}
	}
	for key := range seen {
		c.invalidateKeyLocked(key)
	}
	c.entries = make(map[string]*cacheEntry)
	c.inflight = make(map[string]*call)
}

// InvalidateKey removes a single cached entry.
func (c *ReadCache) InvalidateKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidateKeyLocked(key)
}

// InvalidateMatching removes cached entries whose keys satisfy match.
func (c *ReadCache) InvalidateMatching(match func(string) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := make(map[string]struct{})
	for key := range c.entries {
		if match(key) {
			seen[key] = struct{}{}
		}
	}
	for key := range c.inflight {
		if match(key) {
			seen[key] = struct{}{}
		}
	}
	for key := range seen {
		c.invalidateKeyLocked(key)
	}
}

func (c *ReadCache) runFetch(ctx context.Context, key string, cl *call, fn func(context.Context) ([]byte, error)) {
	fetchCtx, span := readCacheTracer.Start(ctx, "read_cache.fetch")
	defer span.End()
	defer close(cl.done)
	defer func() {
		if r := recover(); r != nil {
			cl.err = fmt.Errorf("read_cache: fetch panicked: %v", r)
		}
		if cl.err != nil {
			span.RecordError(cl.err)
		}

		c.mu.Lock()
		defer c.mu.Unlock()
		if current, ok := c.inflight[key]; ok && current == cl {
			delete(c.inflight, key)
		}
		if cl.err == nil && c.generation[key] == cl.generation {
			c.entries[key] = &cacheEntry{data: cl.val, storedAt: time.Now()}
			c.evictLocked()
		}
	}()

	cl.val, cl.err = fn(fetchCtx)
}

func (c *ReadCache) invalidateKeyLocked(key string) {
	delete(c.entries, key)
	if cl, ok := c.inflight[key]; ok {
		cl.invalidated.Store(true)
	}
	delete(c.inflight, key)
	c.generation[key]++
}

func waitForCall(ctx context.Context, cl *call) ([]byte, error) {
	select {
	case <-cl.done:
		if cl.invalidated.Load() {
			return nil, errReadCacheInvalidated
		}
		return cl.val, cl.err
	case <-ctx.Done():
		select {
		case <-cl.done:
			if cl.invalidated.Load() {
				return nil, errReadCacheInvalidated
			}
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
