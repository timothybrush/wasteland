package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestRenderMutationResult_PrintsAllDetails(t *testing.T) {
	var buf bytes.Buffer
	renderMutationResult(&buf, "Claimed", "w-123", &sdk.MutationResult{
		Branch: "wl/alice/w-123",
		Hint:   "next: wl done w-123",
		Detail: &sdk.DetailResult{
			Item: &commons.WantedItem{
				ID:     "w-123",
				Title:  "Fix auth",
				Status: "claimed",
			},
			BranchURL: "https://example.com/branch",
			PRURL:     "https://example.com/pr/1",
			Delta:     "claimed on branch",
		},
	}, "Mode: pr")

	out := buf.String()
	for _, want := range []string{"Claimed w-123", "Title: Fix auth", "Status: claimed", "Mode: pr", "Branch: wl/alice/w-123", "Branch URL: https://example.com/branch", "PR: https://example.com/pr/1", "Delta: claimed on branch", "next: wl done w-123"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q in %q", want, out)
		}
	}
}
