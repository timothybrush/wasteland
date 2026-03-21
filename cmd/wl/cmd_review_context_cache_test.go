package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestPendingItemsTTLCache_CanceledWaiterDoesNotBlockOnInFlightRefresh(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	cache := newPendingItemsTTLCache(time.Minute, func(context.Context) (map[string][]sdk.PendingItem, error) {
		started <- struct{}{}
		<-release
		return map[string][]sdk.PendingItem{
			"w-1": {{RigHandle: "alice"}},
		}, nil
	})

	firstErrCh := make(chan error, 1)
	go func() {
		_, err := cache(context.Background())
		firstErrCh <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shared refresh did not start")
	}

	waiterCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := cache(waiterCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cache(waiter) error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("canceled waiter blocked for %v waiting on refresh", elapsed)
	}

	close(release)

	select {
	case err := <-firstErrCh:
		if err != nil {
			t.Fatalf("first caller error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first caller did not complete after refresh release")
	}
}

func TestPendingItemsTTLCache_DetachesRefreshFromCanceledCaller(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32

	cache := newPendingItemsTTLCache(time.Minute, func(ctx context.Context) (map[string][]sdk.PendingItem, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return map[string][]sdk.PendingItem{
				"w-1": {{RigHandle: "alice"}},
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErrCh := make(chan error, 1)
	go func() {
		_, err := cache(firstCtx)
		firstErrCh <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shared refresh did not start")
	}

	cancelFirst()

	select {
	case err := <-firstErrCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first caller error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first caller did not return after cancellation")
	}

	secondItemsCh := make(chan map[string][]sdk.PendingItem, 1)
	secondErrCh := make(chan error, 1)
	go func() {
		items, err := cache(context.Background())
		secondItemsCh <- items
		secondErrCh <- err
	}()

	select {
	case <-started:
		t.Fatal("second caller started a new refresh instead of joining the shared one")
	case <-time.After(20 * time.Millisecond):
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch call count = %d, want 1 shared refresh", got)
	}

	close(release)

	select {
	case err := <-secondErrCh:
		if err != nil {
			t.Fatalf("second caller error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second caller did not receive shared refresh result")
	}

	select {
	case items := <-secondItemsCh:
		if len(items["w-1"]) != 1 || items["w-1"][0].RigHandle != "alice" {
			t.Fatalf("second caller items = %+v", items)
		}
	case <-time.After(time.Second):
		t.Fatal("second caller did not receive items")
	}
}
