package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/spf13/cobra"
)

func TestListWantedIDsRemote_HandlesQuotedCommas(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	cfg := &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
	}
	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(string, string) (string, error) {
				return "id,title,priority\nw-123,\"Fix auth, then ship\",1\n", nil
			},
		}, nil
	})

	items := listWantedIDsRemote(cfg, "open")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if got, want := items[0], "w-123\tP1 Fix auth, then ship"; got != want {
		t.Fatalf("item = %q, want %q", got, want)
	}
}

func TestFormatWantedIDCompletions_SkipsBlankRowsAndFormatsTitles(t *testing.T) {
	csv := strings.Join([]string{
		"id,title,priority",
		" ,ignored,1",
		"w-123,Fix auth,1",
		"w-456,Title without priority",
		"w-789," + strings.Repeat("x", 45) + ",2",
		"",
	}, "\n")

	items := formatWantedIDCompletions(csv)
	if len(items) != 3 {
		t.Fatalf("items = %v", items)
	}
	if items[0] != "w-123\tP1 Fix auth" {
		t.Fatalf("items[0] = %q", items[0])
	}
	if items[1] != "w-456\tTitle without priority" {
		t.Fatalf("items[1] = %q", items[1])
	}
	if items[2] != "w-789\tP2 "+strings.Repeat("x", 40)+"..." {
		t.Fatalf("items[2] = %q", items[2])
	}
}

func TestCompleteWantedIDs_CacheScopedByWasteland(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	saveTestConfig(t, &federation.Config{
		Upstream:  "org/one",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})
	saveTestConfig(t, &federation.Config{
		Upstream:  "org/two",
		ForkOrg:   "bob",
		ForkDB:    "wl-commons",
		RigHandle: "bob",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})

	withOpenDBFromConfigOverride(t, func(cfg *federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(string, string) (string, error) {
				switch cfg.Upstream {
				case "org/one":
					return "id,title,priority\nw-one,Alpha,1\n", nil
				case "org/two":
					return "id,title,priority\nw-two,Beta,2\n", nil
				default:
					return "", nil
				}
			},
		}, nil
	})

	fn := completeWantedIDs("open")
	first, _ := fn(commandWithWasteland("org/one"), nil, "")
	second, _ := fn(commandWithWasteland("org/two"), nil, "")

	if len(first) != 1 || first[0] != "w-one\tP1 Alpha" {
		t.Fatalf("first = %v", first)
	}
	if len(second) != 1 || second[0] != "w-two\tP2 Beta" {
		t.Fatalf("second = %v", second)
	}
}

func TestCompleteBranchNames_CacheScopedByWasteland(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	saveTestConfig(t, &federation.Config{
		Upstream:  "org/one",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})
	saveTestConfig(t, &federation.Config{
		Upstream:  "org/two",
		ForkOrg:   "bob",
		ForkDB:    "wl-commons",
		RigHandle: "bob",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})

	withOpenDBFromConfigOverride(t, func(cfg *federation.Config) (commons.DB, error) {
		return scriptedDB{
			branchesFunc: func(string) ([]string, error) {
				if cfg.Upstream == "org/one" {
					return []string{"wl/alice/w-1"}, nil
				}
				return []string{"wl/bob/w-2"}, nil
			},
		}, nil
	})

	first, _ := completeBranchNames(commandWithWasteland("org/one"), nil, "")
	second, _ := completeBranchNames(commandWithWasteland("org/two"), nil, "")

	if len(first) != 1 || first[0] != "wl/alice/w-1" {
		t.Fatalf("first = %v", first)
	}
	if len(second) != 1 || second[0] != "wl/bob/w-2" {
		t.Fatalf("second = %v", second)
	}
}

func TestCompleteProjectNames_CacheScopedByWasteland(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	saveTestConfig(t, &federation.Config{
		Upstream:  "org/one",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})
	saveTestConfig(t, &federation.Config{
		Upstream:  "org/two",
		ForkOrg:   "bob",
		ForkDB:    "wl-commons",
		RigHandle: "bob",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})

	withOpenDBFromConfigOverride(t, func(cfg *federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(string, string) (string, error) {
				if cfg.Upstream == "org/one" {
					return "project\ngastown\n", nil
				}
				return "project\nbeads\n", nil
			},
		}, nil
	})

	first, _ := completeProjectNames(commandWithWasteland("org/one"), nil, "")
	second, _ := completeProjectNames(commandWithWasteland("org/two"), nil, "")

	if len(first) != 1 || first[0] != "gastown" {
		t.Fatalf("first = %v", first)
	}
	if len(second) != 1 || second[0] != "beads" {
		t.Fatalf("second = %v", second)
	}
}

func TestReadCompletionCache_StaleAndInvalidEntriesIgnored(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	writeCompletionCache("fresh", []string{"w-123"})
	if got := readCompletionCache("fresh"); len(got) != 1 || got[0] != "w-123" {
		t.Fatalf("fresh cache = %v", got)
	}

	cacheDir := completionCacheDir()
	stalePath := filepath.Join(cacheDir, "stale.json")
	if err := os.WriteFile(stalePath, []byte(`["old"]`), 0o644); err != nil {
		t.Fatalf("writing stale cache: %v", err)
	}
	old := time.Now().Add(-completionCacheTTL - time.Second)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatalf("dating stale cache: %v", err)
	}
	if got := readCompletionCache("stale"); got != nil {
		t.Fatalf("stale cache = %v, want nil", got)
	}

	invalidPath := filepath.Join(cacheDir, "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("writing invalid cache: %v", err)
	}
	if got := readCompletionCache("invalid"); got != nil {
		t.Fatalf("invalid cache = %v, want nil", got)
	}
}

func TestListWantedIDsWithTimeout_HandlesQuotedCommas(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
query=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-q" ]; then
    query="$2"
    shift 2
    continue
  fi
  shift
done
case "$query" in
  *"SELECT id, title, priority FROM wanted"*)
    printf 'id,title,priority\nw-789,"Ship auth, then docs",0\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	items := listWantedIDsWithTimeout(t.TempDir(), "open")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if got, want := items[0], "w-789\tP0 Ship auth, then docs"; got != want {
		t.Fatalf("item = %q, want %q", got, want)
	}
}

func TestCompletionCacheKey_VariesByConfigScope(t *testing.T) {
	cfgA := &federation.Config{Upstream: "org/one", Backend: federation.BackendRemote}
	cfgB := &federation.Config{Upstream: "org/two", Backend: federation.BackendRemote}
	keyA := completionCacheKey(cfgA, "wanted-open")
	keyB := completionCacheKey(cfgB, "wanted-open")
	if keyA == keyB {
		t.Fatalf("cache key collision: %q", keyA)
	}
	if !strings.HasPrefix(keyA, "wanted-open-") {
		t.Fatalf("cache key = %q", keyA)
	}
}

func TestCompleteWastelandNames_NoConfigs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	items, directive := completeWastelandNames(&cobra.Command{}, nil, "")
	if len(items) != 0 {
		t.Fatalf("items = %v, want empty", items)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v", directive)
	}
}
