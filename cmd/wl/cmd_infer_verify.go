package main

import (
	"fmt"
	"io"

	"github.com/gastownhall/wasteland/internal/inference"
	"github.com/gastownhall/wasteland/internal/sdk"
	"github.com/gastownhall/wasteland/internal/style"
	"github.com/spf13/cobra"
)

var inferVerify = inference.Verify

func newInferVerifyCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <wanted-id>",
		Short: "Re-run an inference job and compare hashes",
		Long: `Verify a completed inference job by re-running it and comparing output hashes.

The item must have type=inference and a submitted completion with evidence.
The job is re-run via ollama with the same parameters, and the output hash
is compared against the claimed result.

Examples:
  wl infer verify w-abc123`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWantedIDs("in_review"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInferVerify(cmd, stdout, stderr, args[0])
		},
	}

	return cmd
}

func runInferVerify(cmd *cobra.Command, stdout, _ io.Writer, wantedID string) error {
	wlCfg, err := resolveWasteland(cmd)
	if err != nil {
		return hintWrap(err)
	}

	wantedID, err = resolveWantedArg(wlCfg, wantedID)
	if err != nil {
		return err
	}

	client, err := newCommandClient(wlCfg, false)
	if err != nil {
		return err
	}

	detail, err := client.Detail(wantedID)
	if err != nil {
		return fmt.Errorf("querying wanted item: %w", err)
	}
	if detail.Item == nil {
		return fmt.Errorf("wanted item %s not found", wantedID)
	}

	vr, err := executeInferVerify(detail, wantedID)
	if err != nil {
		return err
	}

	if vr.Match {
		fmt.Fprintf(stdout, "%s VERIFIED — hashes match\n", style.Success.Render("✓"))
	} else {
		fmt.Fprintf(stdout, "%s MISMATCH — hashes differ\n", style.Error.Render("✗"))
	}
	fmt.Fprintf(stdout, "  Expected: %s\n", vr.ExpectedHash)
	fmt.Fprintf(stdout, "  Actual:   %s\n", vr.ActualHash)
	fmt.Fprintf(stdout, "  Output:   %s\n", truncate(vr.Output, 120))

	return nil
}

// executeInferVerify is the testable business logic for verifying an inference job.
func executeInferVerify(detail *sdk.DetailResult, wantedID string) (*inference.VerifyResult, error) {
	item := detail.Item

	if item.Type != "inference" {
		return nil, fmt.Errorf("wanted item %s has type %q, expected \"inference\"", wantedID, item.Type)
	}

	job, err := inference.DecodeJob(item.Description)
	if err != nil {
		return nil, fmt.Errorf("decoding inference job: %w", err)
	}

	if detail.Completion == nil {
		return nil, fmt.Errorf("no completion found for wanted item %q", wantedID)
	}

	result, err := inference.DecodeResult(detail.Completion.Evidence)
	if err != nil {
		return nil, fmt.Errorf("decoding inference result from evidence: %w", err)
	}

	return inferVerify(job, result)
}
