package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/remote"
	"github.com/gastownhall/wasteland/internal/sdk"
	"github.com/spf13/cobra"
)

func withServeGracefulOverrides(
	t *testing.T,
	listenFn func(*http.Server) error,
	notifyFn func(chan<- os.Signal),
	shutdownFn func(*http.Server, context.Context) error,
) {
	t.Helper()
	oldListen := serveHTTPListen
	oldNotify := serveSignalNotify
	oldShutdown := serveShutdown
	if listenFn != nil {
		serveHTTPListen = listenFn
	}
	if notifyFn != nil {
		serveSignalNotify = notifyFn
	}
	if shutdownFn != nil {
		serveShutdown = shutdownFn
	}
	t.Cleanup(func() {
		serveHTTPListen = oldListen
		serveSignalNotify = oldNotify
		serveShutdown = oldShutdown
	})
}

func TestResolvePort_UsesEnvOverride(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Int("port", 8999, "")
	t.Setenv("PORT", "4321")
	if got := resolvePort(cmd); got != 4321 {
		t.Fatalf("resolvePort() = %d", got)
	}
}

func TestResolvePort_InvalidEnvFallsBackToFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Int("port", 8123, "")
	t.Setenv("PORT", "nope")
	if got := resolvePort(cmd); got != 8123 {
		t.Fatalf("resolvePort() = %d", got)
	}
}

func TestHostedPublicUpstream_IsCanonical(t *testing.T) {
	if hostedPublicUpstreamOrg != "wasteland" {
		t.Fatalf("hostedPublicUpstreamOrg = %q", hostedPublicUpstreamOrg)
	}
	if hostedPublicUpstreamDB != "wl-commons" {
		t.Fatalf("hostedPublicUpstreamDB = %q", hostedPublicUpstreamDB)
	}
	if hostedPublicUpstream != "wasteland/wl-commons" {
		t.Fatalf("hostedPublicUpstream = %q", hostedPublicUpstream)
	}
}

func TestPendingItemsCache_GetAndStop(t *testing.T) {
	cache := &pendingItemsCache{
		cached: map[string][]sdk.PendingItem{
			"w-1": {{RigHandle: "alice"}},
		},
		stop: make(chan struct{}),
	}
	items, err := cache.Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(items["w-1"]) != 1 || items["w-1"][0].RigHandle != "alice" {
		t.Fatalf("items = %+v", items)
	}
	cache.Stop()
	select {
	case <-cache.stop:
	default:
		t.Fatal("stop channel was not closed")
	}
}

func TestNewDetailRefresh_UsesQueryOverride(t *testing.T) {
	old := queryScoreboardDetailEntries
	queryScoreboardDetailEntries = func(_ commons.DB, limit int) ([]commons.ScoreboardDetailEntry, error) {
		if limit != 100 {
			t.Fatalf("limit = %d", limit)
		}
		return []commons.ScoreboardDetailEntry{{
			ScoreboardEntry: commons.ScoreboardEntry{RigHandle: "alice"},
		}}, nil
	}
	t.Cleanup(func() {
		queryScoreboardDetailEntries = old
	})

	data, err := newDetailRefresh(scriptedDB{})()
	if err != nil {
		t.Fatalf("newDetailRefresh() error = %v", err)
	}
	if out := string(data); !strings.Contains(out, `"rig_handle":"alice"`) || !strings.Contains(out, `"entries"`) {
		t.Fatalf("output = %q", out)
	}
}

func TestNewDumpRefresh_UsesQueryOverride(t *testing.T) {
	old := queryScoreboardDumpData
	queryScoreboardDumpData = func(_ commons.DB) (*commons.ScoreboardDump, error) {
		return &commons.ScoreboardDump{
			Rigs: []commons.RigRow{{Handle: "alice"}},
		}, nil
	}
	t.Cleanup(func() {
		queryScoreboardDumpData = old
	})

	data, err := newDumpRefresh(scriptedDB{})()
	if err != nil {
		t.Fatalf("newDumpRefresh() error = %v", err)
	}
	if out := string(data); !strings.Contains(out, `"handle":"alice"`) || !strings.Contains(out, `"rigs"`) {
		t.Fatalf("output = %q", out)
	}
}

func TestNewPendingItemsCache_PrewarmsAndMapsStates(t *testing.T) {
	withPendingWantedStatesOverride(t, func(upstreamOrg, db, token string) (map[string][]remote.PendingWantedState, error) {
		if upstreamOrg != "hop" || db != "wl-commons" || token != "" {
			t.Fatalf("got %q %q %q", upstreamOrg, db, token)
		}
		return map[string][]remote.PendingWantedState{
			"w-1": {{
				RigHandle:   "alice",
				Status:      "in_review",
				ClaimedBy:   "alice",
				Branch:      "wl/alice/w-1",
				BranchURL:   "https://example/branch",
				PRURL:       "https://example/pr/1",
				ForkOwner:   "alice",
				CompletedBy: "alice",
				Evidence:    "https://example/evidence",
			}},
		}, nil
	})

	cache := newPendingItemsCache("hop", "wl-commons", time.Hour)
	defer cache.Stop()

	var got map[string][]sdk.PendingItem
	for i := 0; i < 50; i++ {
		items, err := cache.Get()
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if len(items["w-1"]) == 1 {
			got = items
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got["w-1"]) != 1 || got["w-1"][0].PRURL != "https://example/pr/1" {
		t.Fatalf("items = %+v", got)
	}
}

func TestNewDetailRefresh_PropagatesQueryErrors(t *testing.T) {
	old := queryScoreboardDetailEntries
	queryScoreboardDetailEntries = func(commons.DB, int) ([]commons.ScoreboardDetailEntry, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		queryScoreboardDetailEntries = old
	})

	if _, err := newDetailRefresh(scriptedDB{})(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestPendingItemsCache_GetReturnsCachedMapByReference(t *testing.T) {
	cache := &pendingItemsCache{
		cached: map[string][]sdk.PendingItem{
			"w-2": {{RigHandle: "bob"}},
		},
		stop: make(chan struct{}),
	}
	items, err := cache.Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	items["w-2"][0].RigHandle = "changed"
	again, err := cache.Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if again["w-2"][0].RigHandle != "changed" {
		t.Fatalf("cached map was unexpectedly copied: %+v", again)
	}
}

func TestResolvePort_PrefersEnvInNewServeCmd(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := newServeCmd(&stdout, &stderr)
	t.Setenv("PORT", "7000")
	if got := resolvePort(cmd); got != 7000 {
		t.Fatalf("resolvePort(newServeCmd) = %d", got)
	}
}

func TestInitSentry_NoDSNAndInvalidDSN(t *testing.T) {
	t.Run("no dsn", func(t *testing.T) {
		t.Setenv("SENTRY_DSN", "")
		initSentry("test")
	})

	t.Run("invalid dsn", func(t *testing.T) {
		t.Setenv("SENTRY_DSN", "not-a-dsn")
		initSentry("test")
	})
}

func TestListenAndServeGraceful_SignalPaths(t *testing.T) {
	t.Run("graceful shutdown", func(t *testing.T) {
		done := make(chan struct{})
		withServeGracefulOverrides(
			t,
			func(*http.Server) error {
				<-done
				return http.ErrServerClosed
			},
			func(c chan<- os.Signal) {
				c <- os.Interrupt
			},
			func(*http.Server, context.Context) error {
				close(done)
				return nil
			},
		)

		if err := listenAndServeGraceful(&http.Server{Addr: "127.0.0.1:0"}); err != nil {
			t.Fatalf("listenAndServeGraceful() error = %v", err)
		}
	})

	t.Run("shutdown failure", func(t *testing.T) {
		done := make(chan struct{})
		withServeGracefulOverrides(
			t,
			func(*http.Server) error {
				<-done
				return http.ErrServerClosed
			},
			func(c chan<- os.Signal) {
				c <- os.Interrupt
			},
			func(*http.Server, context.Context) error {
				close(done)
				return errors.New("shutdown boom")
			},
		)

		err := listenAndServeGraceful(&http.Server{Addr: "127.0.0.1:0"})
		if err == nil || !strings.Contains(err.Error(), "graceful shutdown failed: shutdown boom") {
			t.Fatalf("err = %v", err)
		}
	})
}
