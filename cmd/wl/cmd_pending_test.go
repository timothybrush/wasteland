package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestRunPending_ListAndDetail(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	t.Run("list", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				browseFn: func(filter commons.BrowseFilter) (*sdk.BrowseResult, error) {
					if filter.View != "all" || filter.Limit != 25 {
						t.Fatalf("filter = %+v", filter)
					}
					return &sdk.BrowseResult{
						Items: []commons.WantedSummary{
							{ID: "w-1", Title: "Fix auth", Status: "claimed", ClaimedBy: "alice"},
							{ID: "w-2", Title: "Improve review flow", Status: "open"},
						},
						UpstreamPending: map[string][]sdk.PendingItem{
							"w-1": {{
								RigHandle:   "charlie",
								Status:      "in_review",
								Branch:      "wl/charlie/w-1",
								PRURL:       "https://dolthub.example/pr/1",
								Evidence:    "https://github.com/org/repo/pull/99",
								CompletedBy: "charlie",
							}},
						},
					}, nil
				},
			}, nil
		})

		var stdout bytes.Buffer
		if err := runPending(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "", false, 25); err != nil {
			t.Fatalf("runPending(list) error = %v", err)
		}
		for _, want := range []string{"Pending upstream submissions (1 items)", "w-1", "Fix auth", "charlie", "https://dolthub.example/pr/1"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})

	t.Run("detail json", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(wantedID string) (*sdk.DetailResult, error) {
					if wantedID != "w-1" {
						t.Fatalf("wantedID = %q", wantedID)
					}
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:        "w-1",
							Title:     "Fix auth",
							Status:    "claimed",
							ClaimedBy: "alice",
						},
						UpstreamPRs: []sdk.PendingItem{{
							RigHandle:   "charlie",
							Status:      "in_review",
							Branch:      "wl/charlie/w-1",
							BranchURL:   "https://dolthub.example/branch/1",
							PRURL:       "https://dolthub.example/pr/1",
							CompletedBy: "charlie",
							Evidence:    "https://github.com/org/repo/pull/99",
						}},
					}, nil
				},
			}, nil
		})

		var stdout bytes.Buffer
		if err := runPending(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-1", true, 0); err != nil {
			t.Fatalf("runPending(detail json) error = %v", err)
		}

		var report pendingReportJSON
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("json.Unmarshal() error = %v, body = %q", err, stdout.String())
		}
		if len(report.Items) != 1 {
			t.Fatalf("expected 1 item, got %+v", report)
		}
		item := report.Items[0]
		if item.WantedID != "w-1" || item.CurrentClaimedBy != "alice" || len(item.Submissions) != 1 {
			t.Fatalf("item = %+v", item)
		}
		if item.Submissions[0].PRURL != "https://dolthub.example/pr/1" {
			t.Fatalf("submission = %+v", item.Submissions[0])
		}
	})
}
