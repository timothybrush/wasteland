package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadCache_GetMiss(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	if got := c.Get("missing"); got != nil {
		t.Fatalf("expected nil, got %q", got)
	}
}

func TestReadCache_GetOrFetch_CachesResult(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	calls := 0
	fetch := func() ([]byte, error) {
		calls++
		return []byte("hello"), nil
	}

	// First call should invoke fetch.
	got, err := c.GetOrFetch("k", fetch)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// Second call should return cached.
	got, err = c.GetOrFetch("k", fetch)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call after cache hit, got %d", calls)
	}
}

func TestReadCache_TTLExpiry(t *testing.T) {
	c := NewReadCache(10*time.Millisecond, 10)
	_, _ = c.GetOrFetch("k", func() ([]byte, error) {
		return []byte("v1"), nil
	})

	time.Sleep(20 * time.Millisecond)

	// Should be expired.
	if got := c.Get("k"); got != nil {
		t.Fatalf("expected nil after expiry, got %q", got)
	}

	// Should re-fetch.
	got, _ := c.GetOrFetch("k", func() ([]byte, error) {
		return []byte("v2"), nil
	})
	if string(got) != "v2" {
		t.Fatalf("expected %q, got %q", "v2", got)
	}
}

func TestReadCache_GetStale_ReturnsExpiredData(t *testing.T) {
	c := NewReadCache(10*time.Millisecond, 10)
	_, _ = c.GetOrFetch("k", func() ([]byte, error) {
		return []byte("stale"), nil
	})

	time.Sleep(20 * time.Millisecond)

	if got := c.Get("k"); got != nil {
		t.Fatalf("expected expired entry to miss fresh cache, got %q", got)
	}
	if got := c.GetStale("k"); string(got) != "stale" {
		t.Fatalf("GetStale() = %q, want %q", got, "stale")
	}
}

func TestReadCache_ConcurrentCoalescing(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	var fetchCount atomic.Int32

	fetch := func() ([]byte, error) { //nolint:unparam // test helper always succeeds
		fetchCount.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate slow fetch
		return []byte("result"), nil
	}

	var wg sync.WaitGroup
	const n = 20
	results := make([][]byte, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = c.GetOrFetch("same-key", fetch)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d got error: %v", i, errs[i])
		}
		if string(results[i]) != "result" {
			t.Fatalf("goroutine %d got %q", i, results[i])
		}
	}

	// Only one fetch should have executed.
	if got := fetchCount.Load(); got != 1 {
		t.Fatalf("expected 1 fetch, got %d", got)
	}
}

func TestReadCache_Invalidate(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	_, _ = c.GetOrFetch("a", func() ([]byte, error) { return []byte("1"), nil })
	_, _ = c.GetOrFetch("b", func() ([]byte, error) { return []byte("2"), nil })

	c.Invalidate()

	if got := c.Get("a"); got != nil {
		t.Fatalf("expected nil after invalidate, got %q", got)
	}
	if got := c.Get("b"); got != nil {
		t.Fatalf("expected nil after invalidate, got %q", got)
	}
}

func TestReadCache_InvalidateKey(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	_, _ = c.GetOrFetch("a", func() ([]byte, error) { return []byte("1"), nil })
	_, _ = c.GetOrFetch("b", func() ([]byte, error) { return []byte("2"), nil })

	c.InvalidateKey("a")

	if got := c.Get("a"); got != nil {
		t.Fatalf("expected nil for invalidated key, got %q", got)
	}
	if got := c.Get("b"); got == nil {
		t.Fatal("expected non-nil for non-invalidated key")
	}
}

func TestReadCache_InvalidateMatching(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	_, _ = c.GetOrFetch("browse:hop:item", func() ([]byte, error) { return []byte("1"), nil })
	_, _ = c.GetOrFetch("browse:gas:item", func() ([]byte, error) { return []byte("2"), nil })
	_, _ = c.GetOrFetch("detail:hop:item", func() ([]byte, error) { return []byte("3"), nil })

	c.InvalidateMatching(func(key string) bool {
		return strings.HasPrefix(key, "browse:hop:")
	})

	if got := c.Get("browse:hop:item"); got != nil {
		t.Fatalf("expected nil for matched key, got %q", got)
	}
	if got := c.Get("browse:gas:item"); got == nil {
		t.Fatal("expected non-nil for unmatched browse key")
	}
	if got := c.Get("detail:hop:item"); got == nil {
		t.Fatal("expected non-nil for unmatched detail key")
	}
}

func TestReadCache_InvalidateMatching_FencesInflightFetch(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	started := make(chan struct{})
	release := make(chan struct{})
	oldDone := make(chan struct{})
	oldResult := make(chan []byte, 1)
	oldErr := make(chan error, 1)

	go func() {
		defer close(oldDone)
		got, err := c.GetOrFetch("browse:hop:item", func() ([]byte, error) {
			close(started)
			<-release
			return []byte("stale"), nil
		})
		oldResult <- got
		oldErr <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial fetch to start")
	}

	c.InvalidateMatching(func(key string) bool {
		return key == "browse:hop:item"
	})

	freshDone := make(chan struct{})
	var (
		got []byte
		err error
	)
	go func() {
		defer close(freshDone)
		got, err = c.GetOrFetch("browse:hop:item", func() ([]byte, error) {
			return []byte("fresh"), nil
		})
	}()

	select {
	case <-freshDone:
	case <-time.After(time.Second):
		t.Fatal("fresh fetch blocked behind invalidated inflight call")
	}
	if err != nil {
		t.Fatalf("fresh fetch error = %v", err)
	}
	if string(got) != "fresh" {
		t.Fatalf("fresh fetch = %q, want %q", got, "fresh")
	}

	close(release)
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stale fetch to exit")
	}
	if err := <-oldErr; err != nil {
		t.Fatalf("stale caller retry error = %v", err)
	}
	if got := <-oldResult; string(got) != "fresh" {
		t.Fatalf("stale caller got %q, want %q", got, "fresh")
	}

	if got := c.Get("browse:hop:item"); string(got) != "fresh" {
		t.Fatalf("cache = %q, want %q", got, "fresh")
	}
}

func TestReadCache_InvalidateMatching_DoesNotRetryAfterContextCancel(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	started := make(chan struct{})
	release := make(chan struct{})
	var fetchCount atomic.Int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.GetOrFetchContext(ctx, "browse:hop:item", func(context.Context) ([]byte, error) {
			fetchCount.Add(1)
			close(started)
			<-release
			return []byte("stale"), nil
		})
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial fetch to start")
	}

	c.InvalidateMatching(func(key string) bool {
		return key == "browse:hop:item"
	})
	cancel()
	close(release)

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want %v", err, context.Canceled)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled caller")
	}

	if got := fetchCount.Load(); got != 1 {
		t.Fatalf("fetchCount = %d, want 1", got)
	}
}

func TestReadCache_Eviction(t *testing.T) {
	c := NewReadCache(time.Minute, 3)

	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("k%d", i)
		_, _ = c.GetOrFetch(key, func() ([]byte, error) {
			return []byte(key), nil
		})
		// Small sleep so storedAt differs for ordering.
		time.Sleep(2 * time.Millisecond)
	}

	// Should have evicted the oldest entries (k0, k1) keeping k2, k3, k4.
	c.mu.Lock()
	count := len(c.entries)
	c.mu.Unlock()
	if count != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", count)
	}

	// Oldest should be gone.
	if got := c.Get("k0"); got != nil {
		t.Fatalf("expected k0 evicted, got %q", got)
	}
	if got := c.Get("k1"); got != nil {
		t.Fatalf("expected k1 evicted, got %q", got)
	}
	// Newest should remain.
	if got := c.Get("k4"); got == nil {
		t.Fatal("expected k4 to remain")
	}
}

func TestReadCache_FetchError_NotCached(t *testing.T) {
	c := NewReadCache(time.Minute, 10)
	fetchErr := fmt.Errorf("db down")

	_, err := c.GetOrFetch("k", func() ([]byte, error) {
		return nil, fetchErr
	})
	if !errors.Is(err, fetchErr) {
		t.Fatalf("expected fetchErr, got %v", err)
	}

	// Error should not be cached.
	if got := c.Get("k"); got != nil {
		t.Fatalf("expected nil after fetch error, got %q", got)
	}
}

func TestReadCache_FetchPanic_ReturnsErrorAndClearsInflight(t *testing.T) {
	c := NewReadCache(time.Minute, 10)

	_, err := c.GetOrFetch("k", func() ([]byte, error) {
		panic("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "fetch panicked") {
		t.Fatalf("expected panic error, got %v", err)
	}
	if got := c.Get("k"); got != nil {
		t.Fatalf("expected nil after panic, got %q", got)
	}

	c.mu.Lock()
	_, ok := c.inflight["k"]
	c.mu.Unlock()
	if ok {
		t.Fatal("expected inflight entry to be cleared after panic")
	}

	got, err := c.GetOrFetch("k", func() ([]byte, error) {
		return []byte("ok"), nil
	})
	if err != nil {
		t.Fatalf("retry fetch error = %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("retry fetch = %q, want %q", got, "ok")
	}
}
