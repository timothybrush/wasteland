package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestRenderBrowseSummaries_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderBrowseSummaries(&buf, &sdk.BrowseResult{}, false); err != nil {
		t.Fatalf("renderBrowseSummaries() error = %v", err)
	}
	if !strings.Contains(buf.String(), "No wanted items found") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRenderBrowseSummaries_LongAndShort(t *testing.T) {
	result := &sdk.BrowseResult{
		Items: []commons.WantedSummary{{
			ID:          "w-123",
			Title:       "Fix auth",
			Description: "Repair login",
			Project:     "gastown",
			Type:        "bug",
			Priority:    1,
			PostedBy:    "alice",
			Status:      "open",
			EffortLevel: "small",
		}},
	}

	var shortBuf bytes.Buffer
	if err := renderBrowseSummaries(&shortBuf, result, false); err != nil {
		t.Fatalf("short render error = %v", err)
	}
	if !strings.Contains(shortBuf.String(), "Wanted items (1):") || strings.Contains(shortBuf.String(), "DESCRIPTION") {
		t.Fatalf("short output = %q", shortBuf.String())
	}

	var longBuf bytes.Buffer
	if err := renderBrowseSummaries(&longBuf, result, true); err != nil {
		t.Fatalf("long render error = %v", err)
	}
	if !strings.Contains(longBuf.String(), "DESCRIPTION") || !strings.Contains(longBuf.String(), "Repair login") {
		t.Fatalf("long output = %q", longBuf.String())
	}
}

func TestRenderBrowseJSON(t *testing.T) {
	var buf bytes.Buffer
	result := &sdk.BrowseResult{
		Items: []commons.WantedSummary{{ID: "w-123", Title: "Fix auth"}},
	}
	if err := renderBrowseJSON(&buf, result); err != nil {
		t.Fatalf("renderBrowseJSON() error = %v", err)
	}
	var decoded []commons.WantedSummary
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(decoded) != 1 || decoded[0].ID != "w-123" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestRenderBrowseCSV(t *testing.T) {
	var buf bytes.Buffer
	csvData := "id,title,project,type,priority,posted_by,claimed_by,status,effort_level\nw-123,Fix auth,gastown,bug,1,alice,,open,small\n"
	if err := renderBrowseCSV(&buf, csvData); err != nil {
		t.Fatalf("renderBrowseCSV() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Wanted items (1):") || !strings.Contains(out, "Fix auth") {
		t.Fatalf("output = %q", out)
	}
}

func TestRunBrowseRemote_RendersResults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		Mode:      federation.ModeWildWest,
		JoinedAt:  time.Now(),
	})
	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(string, string) (string, error) {
				return "id,title,description,project,type,priority,posted_by,claimed_by,status,effort_level\nw-123,Fix auth,Repair login,gastown,bug,1,alice,,open,small\n", nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runBrowse(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, commons.BrowseFilter{Limit: 50}, false, false); err != nil {
		t.Fatalf("runBrowse() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Wanted items (1):") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
