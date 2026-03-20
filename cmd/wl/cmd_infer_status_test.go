package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/sdk"
)

func TestRenderInferStatus_WithDecodedJobAndResult(t *testing.T) {
	var buf bytes.Buffer
	renderInferStatus(&buf, &sdk.DetailResult{
		Item: &commons.WantedItem{
			ID:          "w-123",
			Title:       "Run inference",
			Status:      "completed",
			Type:        "inference",
			Description: `{"prompt":"Summarize the auth diff","model":"gpt-5","seed":7,"max_tokens":128}`,
			PostedBy:    "alice",
			ClaimedBy:   "bob",
		},
		Completion: &commons.CompletionRecord{
			Evidence: `{"output":"done","output_hash":"abc123","model":"gpt-5","seed":7}`,
		},
	})

	out := buf.String()
	for _, want := range []string{"Inference Job:", "Model:      gpt-5", "Seed:       7", "Max tokens: 128", "Inference Result:", "Hash:   abc123", "Posted by: alice", "Claimed by: bob"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q in %q", want, out)
		}
	}
}

func TestRenderInferStatus_FallsBackToDescription(t *testing.T) {
	var buf bytes.Buffer
	renderInferStatus(&buf, &sdk.DetailResult{
		Item: &commons.WantedItem{
			ID:          "w-123",
			Title:       "Run inference",
			Status:      "open",
			Type:        "inference",
			Description: "plain description",
		},
	})
	if !strings.Contains(buf.String(), "Description: plain description") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRenderInferStatus_TruncatesLongPromptAndOutput(t *testing.T) {
	var buf bytes.Buffer
	longPrompt := strings.Repeat("prompt ", 20)
	longOutput := strings.Repeat("output ", 25)
	renderInferStatus(&buf, &sdk.DetailResult{
		Item: &commons.WantedItem{
			ID:          "w-456",
			Title:       "Long inference",
			Status:      "completed",
			Type:        "inference",
			Description: `{"prompt":"` + longPrompt + `","model":"gpt-5","seed":7,"max_tokens":0}`,
		},
		Completion: &commons.CompletionRecord{
			Evidence: `{"output":"` + longOutput + `","output_hash":"abc123","model":"gpt-5","seed":7}`,
		},
	})

	out := buf.String()
	if !strings.Contains(out, "Prompt:     "+truncate(longPrompt, 100)) {
		t.Fatalf("prompt output = %q", out)
	}
	if !strings.Contains(out, "Output: "+truncate(longOutput, 120)) {
		t.Fatalf("result output = %q", out)
	}
}
