package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
)

func TestRunBrowse_LocalEphemeralPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
case "$1" in
  clone)
    mkdir -p "$3"
    ;;
  sql)
    printf 'id,title,project,type,priority,posted_by,claimed_by,status,effort_level\nw-123,Fix auth,gastown,bug,1,alice,,open,small\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")

	var stdout bytes.Buffer
	if err := runBrowse(cmd, &stdout, io.Discard, commons.BrowseFilter{Limit: 50}, false, true); err != nil {
		t.Fatalf("runBrowse() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Wanted items (1):") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunBrowseLocal_PRJSONAndPullError(t *testing.T) {
	t.Run("pr json", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		saveTestConfig(t, &federation.Config{
			Upstream:     "hop/wl-commons",
			ForkOrg:      "alice",
			ForkDB:       "wl-commons",
			LocalDir:     t.TempDir(),
			RigHandle:    "alice",
			Backend:      federation.BackendLocal,
			Mode:         federation.ModePR,
			ProviderType: "dolthub",
			JoinedAt:     time.Now(),
		})
		installFakeDolt(t, `#!/bin/sh
set -eu
args="$*"
case "$args" in
  "pull upstream main"|"fetch origin")
    exit 0
    ;;
  *)
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
      *"SELECT name FROM dolt_remote_branches WHERE name LIKE 'remotes/origin/wl/%'"*)
        printf 'name\n'
        ;;
      *)
        exit 1
        ;;
    esac
    ;;
esac
`)
		withOpenDBOverride(t, func(string) commons.DB {
			return scriptedDB{
				queryFunc: func(string, string) (string, error) {
					return "id,title,description,project,type,priority,posted_by,claimed_by,status,effort_level\nw-123,Fix auth,Repair login,gastown,bug,1,alice,,open,small\n", nil
				},
			}
		})
		withPendingWantedStatesOverride(t, func(string, string, string) (map[string][]remote.PendingWantedState, error) {
			return map[string][]remote.PendingWantedState{}, nil
		})

		var stdout bytes.Buffer
		if err := runBrowseLocal(&stdout, io.Discard, commandTestConfig(t), commons.BrowseFilter{Limit: 50}, true); err != nil {
			t.Fatalf("runBrowseLocal() error = %v", err)
		}
		if out := stdout.String(); !strings.Contains(out, `"id": "w-123"`) {
			t.Fatalf("stdout = %q", out)
		}
	})

	t.Run("pull error", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
		installFakeDolt(t, "#!/bin/sh\nexit 1\n")

		err := runBrowseLocal(io.Discard, io.Discard, commandTestConfig(t), commons.BrowseFilter{Limit: 50}, false)
		if err == nil || !strings.Contains(err.Error(), "pulling upstream") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRunBrowseRemote_PRJSONAndOpenDBError(t *testing.T) {
	t.Run("pr json", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		saveTestConfig(t, &federation.Config{
			Upstream:     "hop/wl-commons",
			ForkOrg:      "alice",
			ForkDB:       "wl-commons",
			RigHandle:    "alice",
			Backend:      federation.BackendRemote,
			Mode:         federation.ModePR,
			ProviderType: "dolthub",
			JoinedAt:     time.Now(),
		})
		withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
			return scriptedDB{
				queryFunc: func(string, string) (string, error) {
					return "id,title,description,project,type,priority,posted_by,claimed_by,status,effort_level\nw-321,Review API,Polish browse,gastown,feature,2,bob,,open,medium\n", nil
				},
			}, nil
		})
		withPendingWantedStatesOverride(t, func(string, string, string) (map[string][]remote.PendingWantedState, error) {
			return map[string][]remote.PendingWantedState{}, nil
		})

		var stdout bytes.Buffer
		if err := runBrowseRemote(&stdout, io.Discard, commandTestConfig(t), commons.BrowseFilter{Limit: 50}, true); err != nil {
			t.Fatalf("runBrowseRemote() error = %v", err)
		}
		if out := stdout.String(); !strings.Contains(out, `"id": "w-321"`) {
			t.Fatalf("stdout = %q", out)
		}
	})

	t.Run("open db error", func(t *testing.T) {
		withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
			return nil, errors.New("boom")
		})
		err := runBrowseRemote(io.Discard, io.Discard, &federation.Config{}, commons.BrowseFilter{Limit: 50}, false)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestNewBrowseClientConfig_PRWiresCallbacks(t *testing.T) {
	t.Setenv("DOLTHUB_TOKEN", "token")
	cfg := &federation.Config{
		Upstream:     "hop/wl-commons",
		ForkOrg:      "alice",
		ForkDB:       "wl-commons",
		RigHandle:    "alice",
		Backend:      federation.BackendRemote,
		Mode:         federation.ModePR,
		ProviderType: "dolthub",
	}
	clientCfg := newBrowseClientConfig(cfg, scriptedDB{})
	if clientCfg.LoadPendingDetail == nil || clientCfg.ListPendingItems == nil {
		t.Fatalf("clientCfg = %+v", clientCfg)
	}
}

func TestRunBrowse_LocalMissingDolt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  t.TempDir(),
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		JoinedAt:  time.Now(),
	})

	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")
	err := runBrowse(cmd, io.Discard, io.Discard, commons.BrowseFilter{Limit: 50}, false, false)
	if err == nil || !strings.Contains(err.Error(), "dolt") {
		t.Fatalf("err = %v", err)
	}
}
