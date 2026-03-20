package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
)

func TestRunBrowseLocal_RendersResults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  pull)
    exit 0
    ;;
  *)
    exit 1
    ;;
esac
`)
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  t.TempDir(),
		RigHandle: "alice",
		Backend:   federation.BackendLocal,
		Mode:      federation.ModeWildWest,
		JoinedAt:  time.Now(),
	})
	withOpenDBOverride(t, func(string) commons.DB {
		return scriptedDB{
			queryFunc: func(string, string) (string, error) {
				return "id,title,description,project,type,priority,posted_by,claimed_by,status,effort_level\nw-123,Fix auth,Repair login,gastown,bug,1,alice,,open,small\n", nil
			},
		}
	})
	cmd := commandWithWasteland("hop/wl-commons")
	_ = cmd.Flags().Set("local-db", "true")

	var stdout bytes.Buffer
	if err := runBrowse(cmd, &stdout, io.Discard, commons.BrowseFilter{Limit: 50}, false, false); err != nil {
		t.Fatalf("runBrowse() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Wanted items (1):") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestNewBrowseClientConfig_WildWestDisablesPRCallbacks(t *testing.T) {
	cfg := &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Mode:      federation.ModeWildWest,
	}
	clientCfg := newBrowseClientConfig(cfg, scriptedDB{})
	if clientCfg.LoadPendingDetail != nil || clientCfg.ListPendingItems != nil {
		t.Fatalf("clientCfg should not wire PR callbacks in wild-west mode")
	}
}

func TestRunBrowseEphemeral_JSON(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  clone)
    mkdir -p "$3"
    exit 0
    ;;
  sql)
    printf '[{"id":"w-123","title":"Fix auth"}]\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	var stdout bytes.Buffer
	err := runBrowseEphemeral(&stdout, &federation.Config{Upstream: "hop/wl-commons"}, "SELECT 1", true)
	if err != nil {
		t.Fatalf("runBrowseEphemeral() error = %v", err)
	}
	if !strings.Contains(stdout.String(), `"id":"w-123"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunBrowseEphemeral_Table(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
case "$1" in
  clone)
    mkdir -p "$3"
    exit 0
    ;;
  sql)
    printf 'id,title,project,type,priority,posted_by,claimed_by,status,effort_level\nw-123,Fix auth,gastown,bug,1,alice,,open,small\n'
    ;;
  *)
    exit 1
    ;;
esac
`)

	var stdout bytes.Buffer
	err := runBrowseEphemeral(&stdout, &federation.Config{Upstream: "hop/wl-commons"}, "SELECT 1", false)
	if err != nil {
		t.Fatalf("runBrowseEphemeral() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Wanted items (1):") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRenderBrowseTable_ReportsQueryFailure(t *testing.T) {
	installFakeDolt(t, `#!/bin/sh
set -eu
echo 'query exploded' >&2
exit 1
`)

	err := renderBrowseTable(io.Discard, "dolt", t.TempDir(), "SELECT 1")
	if err == nil {
		t.Fatal("renderBrowseTable() expected error")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderBrowseCSV_NoRowsAndShortRows(t *testing.T) {
	t.Run("no rows", func(t *testing.T) {
		var stdout bytes.Buffer
		if err := renderBrowseCSV(&stdout, "id,title\n"); err != nil {
			t.Fatalf("renderBrowseCSV() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "No wanted items found") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("short rows skipped", func(t *testing.T) {
		var stdout bytes.Buffer
		csvData := "id,title,project,type,priority,posted_by,claimed_by,status,effort_level\nw-1,Good,gastown,bug,1,alice,,open,small\nw-2,Short\n"
		if err := renderBrowseCSV(&stdout, csvData); err != nil {
			t.Fatalf("renderBrowseCSV() error = %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "Wanted items (2):") || !strings.Contains(out, "Good") {
			t.Fatalf("stdout = %q", out)
		}
		if strings.Contains(out, "Short") {
			t.Fatalf("short row should have been skipped: %q", out)
		}
	})
}
