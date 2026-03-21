package ctxutil

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDetached_IgnoresParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	detached, cancelDetached := Detached(parent, 50*time.Millisecond)
	defer cancelDetached()

	cancelParent()

	select {
	case <-detached.Done():
		t.Fatal("detached context canceled with parent cancellation")
	default:
	}
}

func TestDetached_IgnoresParentDeadline(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelParent()

	detached, cancelDetached := Detached(parent, 80*time.Millisecond)
	defer cancelDetached()

	select {
	case <-detached.Done():
		t.Fatal("detached context should not inherit parent deadline")
	case <-time.After(40 * time.Millisecond):
	}

	select {
	case <-detached.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatal("detached context did not respect fallback timeout")
	}

	if !errors.Is(detached.Err(), context.DeadlineExceeded) {
		t.Fatalf("detached.Err() = %v, want context.DeadlineExceeded", detached.Err())
	}
}

func TestDetached_UsesFallbackTimeoutWithoutParentDeadline(t *testing.T) {
	detached, cancelDetached := Detached(context.Background(), 20*time.Millisecond)
	defer cancelDetached()

	select {
	case <-detached.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatal("detached context did not use fallback timeout")
	}

	if !errors.Is(detached.Err(), context.DeadlineExceeded) {
		t.Fatalf("detached.Err() = %v, want context.DeadlineExceeded", detached.Err())
	}
}

func TestDetached_DefaultsFallbackWhenNonPositive(t *testing.T) {
	detached, cancelDetached := Detached(context.Background(), 0)
	defer cancelDetached()

	deadline, ok := detached.Deadline()
	if !ok {
		t.Fatal("detached context missing deadline")
	}

	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatalf("remaining timeout = %v, want > 0", remaining)
	}
	if remaining > defaultDetachedTimeout+time.Second {
		t.Fatalf("remaining timeout = %v, unexpectedly larger than default bound", remaining)
	}
}
