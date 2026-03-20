package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
)

func TestRunMeRemote_PrintsDashboardSections(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})
	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(query, _ string) (string, error) {
				switch {
				case strings.Contains(query, "status IN ('claimed','in_review')"):
					return "id,title,status,priority,effort_level\nw-1,Fix auth,claimed,1,small\n", nil
				case strings.Contains(query, "posted_by = 'alice' AND status = 'in_review'"):
					return "id,title,claimed_by\nw-2,Review UI,bob\n", nil
				case strings.Contains(query, "claimed_by = 'alice' AND status = 'completed'"):
					return "id,title\nw-3,Ship docs\n", nil
				default:
					return "", fmt.Errorf("unexpected query: %s", query)
				}
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runMe(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard); err != nil {
		t.Fatalf("runMe() error = %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"Claimed items:", "Awaiting my review:", "Recent completions:", "w-1", "w-2", "w-3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in %q", want, out)
		}
	}
}

func TestRunMeRemote_EmptyState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})
	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(query, _ string) (string, error) {
				switch {
				case strings.Contains(query, "status IN ('claimed','in_review')"):
					return "id,title,status,priority,effort_level\n", nil
				case strings.Contains(query, "posted_by = 'alice' AND status = 'in_review'"):
					return "id,title,claimed_by\n", nil
				case strings.Contains(query, "claimed_by = 'alice' AND status = 'completed'"):
					return "id,title\n", nil
				default:
					return "", nil
				}
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runMe(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard); err != nil {
		t.Fatalf("runMe() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Nothing here") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestQueryClaimedAsOf_ParsesRows(t *testing.T) {
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
  *"SELECT id, title, status, priority, effort_level, DATEDIFF"*)
    printf 'id,title,status,priority,effort_level,days_stale\nw-1,Fix auth,claimed,1,small,8\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	rows := queryClaimedAsOf(t.TempDir(), "alice", "upstream/main")
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0][0] != "w-1" || rows[0][5] != "8" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestRemoteBranchSetAndListBranchItems(t *testing.T) {
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
  *"SELECT name FROM dolt_remote_branches"*)
    printf 'name\nremotes/origin/wl/alice/w-2\nremotes/upstream/wl/alice/w-2\n'
    ;;
  *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/alice/%'"*)
    printf 'name\nwl/alice/w-2\n'
    ;;
  *"SELECT id, title, status FROM wanted AS OF 'wl/alice/w-2'"*)
    printf 'id,title,status\nw-2,Review auth,claimed\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	remotes := remoteBranchSet(t.TempDir())
	if got := branchLocation(remotes, "wl/alice/w-2"); got != "origin + upstream" {
		t.Fatalf("branchLocation = %q", got)
	}

	items := listBranchItems(t.TempDir(), "alice", map[string]bool{})
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	if items[0].id != "w-2" || items[0].location != "origin + upstream" {
		t.Fatalf("items = %+v", items)
	}
}

func TestBranchLocation_Variants(t *testing.T) {
	remotes := map[string]map[string]bool{
		"wl/alice/w-origin":   {"origin": true},
		"wl/alice/w-upstream": {"upstream": true},
	}
	if got := branchLocation(remotes, "wl/alice/w-origin"); got != "origin" {
		t.Fatalf("origin branchLocation = %q", got)
	}
	if got := branchLocation(remotes, "wl/alice/w-upstream"); got != "upstream" {
		t.Fatalf("upstream branchLocation = %q", got)
	}
	if got := branchLocation(remotes, "wl/alice/w-local"); got != "local only" {
		t.Fatalf("local branchLocation = %q", got)
	}
}

func TestListBranchItems_SkipsSeenAndBrokenBranches(t *testing.T) {
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
  *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/alice/%'"*)
    printf 'name\nwl/alice/w-seen\nwl/alice/w-broken\nwl/alice/w-good\n'
    ;;
  "SELECT name FROM dolt_remote_branches")
    printf 'name\nremotes/origin/wl/alice/w-good\n'
    ;;
  *"SELECT id, title, status FROM wanted AS OF 'wl/alice/w-seen'"*)
    printf 'id,title,status\nw-seen,Seen item,claimed\n'
    ;;
  *"SELECT id, title, status FROM wanted AS OF 'wl/alice/w-broken'"*)
    exit 1
    ;;
  *"SELECT id, title, status FROM wanted AS OF 'wl/alice/w-good'"*)
    printf 'id,title,status\nw-good,Good item,claimed\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	items := listBranchItems(t.TempDir(), "alice", map[string]bool{"w-seen": true})
	if len(items) != 1 || items[0].id != "w-good" || items[0].location != "origin" {
		t.Fatalf("items = %+v", items)
	}
}

func TestStaleWarningAndExtractBranchWantedID(t *testing.T) {
	if got := staleWarning("7"); got != "" {
		t.Fatalf("staleWarning(7) = %q, want empty", got)
	}
	if got := staleWarning("8"); !strings.Contains(got, "wl done or wl unclaim") {
		t.Fatalf("staleWarning(8) = %q", got)
	}
	if got := extractBranchWantedID("wl/alice/w-123"); got != "w-123" {
		t.Fatalf("extractBranchWantedID = %q", got)
	}
	if got := extractBranchWantedID("main"); got != "" {
		t.Fatalf("extractBranchWantedID(main) = %q", got)
	}
}

func TestRunMe_Local_PrintsAllSections(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	localDir := t.TempDir()
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  localDir,
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})

	installFakeDolt(t, `#!/bin/sh
set -eu
cmd="$1"
shift
case "$cmd:$*" in
  "fetch:upstream"|"fetch:origin")
    exit 0
    ;;
  sql*)
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
      *"FROM wanted AS OF 'upstream/main'"*"claimed_by = 'alice'"*"status = 'completed'"*)
        printf 'id,title\nw-done,Ship it\n'
        ;;
      *"FROM wanted AS OF 'upstream/main'"*"claimed_by = 'alice'"*)
        printf 'id,title,status,priority,effort_level,days_stale\nw-up,Fix upstream,claimed,1,small,8\n'
        ;;
      *"FROM wanted AS OF 'origin/main'"*"claimed_by = 'alice'"*)
        printf 'id,title,status,priority,effort_level,days_stale\nw-up,Fix upstream,claimed,1,small,8\nw-origin,Fix fork,claimed,2,medium,2\n'
        ;;
      "SELECT name FROM dolt_remote_branches")
        printf 'name\nremotes/origin/wl/alice/w-branch\nremotes/upstream/wl/alice/w-branch\n'
        ;;
      *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/alice/%'"*)
        printf 'name\nwl/alice/w-branch\n'
        ;;
      *"SELECT id, title, status FROM wanted AS OF 'wl/alice/w-branch'"*)
        printf 'id,title,status\nw-branch,Branch work,claimed\n'
        ;;
      *"posted_by = 'alice' AND status = 'in_review'"*)
        printf 'id,title,claimed_by\nw-review,Review this,bob\n'
        ;;
      *)
        exit 1
        ;;
    esac
    ;;
  *)
    exit 1
    ;;
esac
`)

	var stdout bytes.Buffer
	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")
	if err := runMe(cmd, &stdout, io.Discard); err != nil {
		t.Fatalf("runMe(local) error = %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"On upstream (master DB):",
		"On origin only (your fork — not yet on upstream):",
		"On branches (origin + upstream, not merged to main):",
		"Awaiting my review:",
		"Recent completions:",
		"w-up",
		"w-origin",
		"w-branch",
		"w-review",
		"w-done",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q in %q", want, out)
		}
	}
}
