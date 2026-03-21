package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestReadCache_LeaderCancellation(t *testing.T) {
	c := NewReadCache(5*time.Minute, 10)

	leaderCtx, leaderCancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	waitLeader := make(chan struct{})
	leaderDone := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Add(1)

	// Leader
	go func() {
		defer wg.Done()
		_, err := c.GetOrFetchContext(leaderCtx, "key1", func(ctx context.Context) ([]byte, error) {
			close(started)
			<-waitLeader          // simulate work
			return nil, ctx.Err() // returns error if canceled
		})
		leaderDone <- err
	}()

	<-started // wait for leader to enter fn

	// Follower
	followerCtx := context.Background()
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Sleep a bit to ensure it hits the inflight path
		time.Sleep(10 * time.Millisecond)
		_, err := c.GetOrFetchContext(followerCtx, "key1", func(_ context.Context) ([]byte, error) {
			return []byte("success"), nil
		})
		if errors.Is(err, context.Canceled) {
			t.Errorf("follower got leader's cancellation error!")
		} else if err != nil {
			t.Errorf("follower err = %v", err)
		}
	}()

	// Cancel leader
	leaderCancel()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Errorf("leader err = %v, want context.Canceled", err)
	}
	close(waitLeader)

	wg.Wait()
}
