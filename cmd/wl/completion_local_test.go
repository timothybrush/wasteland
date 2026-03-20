package main

import (
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/federation"
)

func TestCompleteWantedIDs_LocalPathAndArgHandling(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  t.TempDir(),
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})
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
  *"SELECT id, title, priority FROM wanted WHERE status = 'claimed'"*)
    printf 'id,title,priority\nw-111,Fix auth,1\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	fn := completeWantedIDs("claimed")
	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")
	items, _ := fn(cmd, nil, "")
	if len(items) != 1 || items[0] != "w-111\tP1 Fix auth" {
		t.Fatalf("items = %v", items)
	}

	skipped, _ := fn(cmd, []string{"already"}, "")
	if skipped != nil {
		t.Fatalf("skipped = %v, want nil", skipped)
	}
}

func TestCompleteBranchNames_LocalPathAndArgHandling(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  t.TempDir(),
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})
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
  *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/%'"*)
    printf 'name\nwl/alice/w-1\nwl/alice/w-2\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")
	branches, _ := completeBranchNames(cmd, nil, "")
	if len(branches) != 2 || branches[1] != "wl/alice/w-2" {
		t.Fatalf("branches = %v", branches)
	}

	skipped, _ := completeBranchNames(cmd, []string{"extra"}, "")
	if skipped != nil {
		t.Fatalf("skipped = %v, want nil", skipped)
	}
}

func TestCompleteProjectNames_LocalAndResolveFailure(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("TMPDIR", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
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
  *"SELECT DISTINCT project FROM wanted WHERE project != '' ORDER BY project LIMIT 50"*)
    printf 'project\ngastown\nbeads\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

		cmd := commandWithWasteland("hop/wl-commons")
		_ = cmd.Flags().Set("local-db", "true")
		projects, _ := completeProjectNames(cmd, nil, "")
		if len(projects) != 2 || projects[0] != "gastown" {
			t.Fatalf("projects = %v", projects)
		}
	})

	t.Run("resolve failure", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		items, _ := completeProjectNames(commandWithWasteland("missing/upstream"), nil, "")
		if items != nil {
			t.Fatalf("items = %v, want nil", items)
		}
	})
}

func TestDoltQueryWithTimeout_ErrorReturnsEmpty(t *testing.T) {
	installFakeDolt(t, "#!/bin/sh\nexit 1\n")
	if got := doltQueryWithTimeout(t.TempDir(), "SELECT 1", time.Second); got != "" {
		t.Fatalf("got = %q", got)
	}
}
