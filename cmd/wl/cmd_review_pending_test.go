package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
)

func TestDolthubListPendingItems_MapsAndCachesResults(t *testing.T) {
	t.Setenv("DOLTHUB_TOKEN", "token")

	var calls int
	withPendingWantedStatesOverride(t, func(upstreamOrg, db, token string) (map[string][]remote.PendingWantedState, error) {
		calls++
		if upstreamOrg != "hop" || db != "wl-commons" || token != "token" {
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

	cb := dolthubListPendingItems(&federation.Config{Upstream: "hop/wl-commons"})
	if cb == nil {
		t.Fatal("callback is nil")
	}

	first, err := cb()
	if err != nil {
		t.Fatalf("first call error = %v", err)
	}
	second, err := cb()
	if err != nil {
		t.Fatalf("second call error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if len(first["w-1"]) != 1 || first["w-1"][0].Branch != "wl/alice/w-1" {
		t.Fatalf("first = %+v", first)
	}
	if len(second["w-1"]) != 1 || second["w-1"][0].PRURL != "https://example/pr/1" {
		t.Fatalf("second = %+v", second)
	}
}

func TestGHListPendingItems_ParsesBranchAndFallbackWantedIDs(t *testing.T) {
	endpointFile := filepath.Join(t.TempDir(), "endpoint.txt")
	countFile := filepath.Join(t.TempDir(), "count.txt")

	ghPath := installFakeCommand(t, "gh", `#!/bin/sh
set -eu
if [ "$1" = "api" ] && [ "$2" = "--paginate" ]; then
  printf '%s' "$3" > `+shellQuote(endpointFile)+`
  count=0
  if [ -f `+shellQuote(countFile)+` ]; then
    count=$(cat `+shellQuote(countFile)+`)
  fi
  count=$((count+1))
  printf '%s' "$count" > `+shellQuote(countFile)+`
  cat <<'EOF'
[
  {"head":{"ref":"wl/alice/w-go-1"},"title":"Auth fix","user":{"login":"alice"}},
  {"head":{"ref":"feature/w-api-2"},"title":"Carry w-api-2 through","user":{"login":"bob"}},
  {"head":{"ref":"feature/no-id"},"title":"Ship w-web-3 today","user":{"login":"carol"}},
  {"head":{"ref":"feature/no-match"},"title":"No wanted id here","user":{"login":"nobody"}}
]
EOF
  exit 0
fi
exit 1
`)

	cb := ghListPendingItems(ghPath, "org/repo")
	first, err := cb()
	if err != nil {
		t.Fatalf("first call error = %v", err)
	}
	second, err := cb()
	if err != nil {
		t.Fatalf("second call error = %v", err)
	}

	if len(first["w-go-1"]) != 1 || first["w-go-1"][0].RigHandle != "alice" {
		t.Fatalf("first = %+v", first)
	}
	if len(first["w-api-2"]) != 1 || first["w-api-2"][0].RigHandle != "bob" {
		t.Fatalf("first = %+v", first)
	}
	if len(first["w-web-3"]) != 1 || first["w-web-3"][0].RigHandle != "carol" {
		t.Fatalf("first = %+v", first)
	}
	if _, ok := first[""]; ok {
		t.Fatalf("unexpected empty wanted ID entry: %+v", first)
	}
	if len(second) != len(first) {
		t.Fatalf("cache changed result size: first=%d second=%d", len(first), len(second))
	}

	if endpoint, err := os.ReadFile(endpointFile); err != nil || string(endpoint) != "repos/org/repo/pulls?state=open&per_page=100" {
		t.Fatalf("endpoint = %q, err = %v", string(endpoint), err)
	}
	if count, err := os.ReadFile(countFile); err != nil || strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("count = %q, err = %v", string(count), err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
