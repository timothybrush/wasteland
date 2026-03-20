package main

import (
	"fmt"
	"io"

	"github.com/gastownhall/wasteland/internal/inference"
	"github.com/gastownhall/wasteland/internal/style"
	"github.com/spf13/cobra"
)

var inferRun = inference.Run

func newInferRunCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		noPush    bool
		skipClaim bool
	)

	cmd := &cobra.Command{
		Use:   "run <wanted-id>",
		Short: "Claim and execute an inference job via ollama",
		Long: `Claim a wanted inference item and run it against the local ollama instance.

The item must have type=inference and status=open (or status=claimed with
--skip-claim). The job parameters are decoded from the description field.
On success, the result (with SHA-256 hash) is submitted as completion
evidence. On failure without --skip-claim, the claim is released so
another worker can retry.

Use --skip-claim when the item was already claimed externally (e.g., by
the wasteland-feeder automation).

Examples:
  wl infer run w-abc123
  wl infer run w-abc123 --skip-claim
  wl infer run w-abc123 --no-push`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWantedIDs("open"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInferRun(cmd, stdout, stderr, args[0], noPush, skipClaim)
		},
	}

	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	cmd.Flags().BoolVar(&skipClaim, "skip-claim", false, "Skip claiming (item already claimed externally)")

	return cmd
}

func runInferRun(cmd *cobra.Command, stdout, _ io.Writer, wantedID string, noPush, skipClaim bool) error {
	wlCfg, err := resolveWasteland(cmd)
	if err != nil {
		return hintWrap(err)
	}

	wantedID, err = resolveWantedArg(wlCfg, wantedID)
	if err != nil {
		return err
	}

	client, err := newCommandClient(wlCfg, noPush)
	if err != nil {
		return err
	}

	// Step 1: branch-aware read to get the item description for job decoding.
	detail, err := client.Detail(wantedID)
	if err != nil {
		return fmt.Errorf("querying wanted item: %w", err)
	}
	if detail.Item == nil {
		return fmt.Errorf("wanted item %s not found", wantedID)
	}
	if detail.Item.Type != "inference" {
		return fmt.Errorf("wanted item %s has type %q, expected \"inference\"", wantedID, detail.Item.Type)
	}

	// Validate status.
	if skipClaim {
		if detail.Item.Status != "claimed" {
			return fmt.Errorf("wanted item %s has status %q, expected \"claimed\" (--skip-claim)", wantedID, detail.Item.Status)
		}
	} else {
		if detail.Item.Status != "open" {
			return fmt.Errorf("wanted item %s has status %q, expected \"open\"", wantedID, detail.Item.Status)
		}
	}

	// Decode job from description.
	job, err := inference.DecodeJob(detail.Item.Description)
	if err != nil {
		return fmt.Errorf("decoding inference job from description: %w", err)
	}

	// Step 2: Claim (unless --skip-claim).
	if !skipClaim {
		if _, err := client.Claim(wantedID); err != nil {
			return fmt.Errorf("claiming wanted item: %w", err)
		}
	}

	// Step 3: Run inference.
	result, err := inferRun(job)
	if err != nil {
		if !skipClaim {
			// Release claim so another worker can retry.
			_, _ = client.Unclaim(wantedID)
		}
		return fmt.Errorf("running inference: %w", err)
	}

	// Encode result as evidence.
	evidence, err := inference.EncodeResult(result)
	if err != nil {
		if !skipClaim {
			_, _ = client.Unclaim(wantedID)
		}
		return fmt.Errorf("encoding inference result: %w", err)
	}

	// Step 4: Submit completion.
	doneResult, err := client.Done(wantedID, evidence)
	if err != nil {
		if !skipClaim {
			_, _ = client.Unclaim(wantedID)
		}
		return fmt.Errorf("submitting completion: %w", err)
	}

	fmt.Fprintf(stdout, "%s Inference completed for %s\n", style.Bold.Render("✓"), wantedID)
	fmt.Fprintf(stdout, "  Completed by:  %s\n", wlCfg.RigHandle)
	if doneResult.Branch != "" {
		fmt.Fprintf(stdout, "  Branch: %s\n", doneResult.Branch)
	}
	if doneResult.Detail != nil && doneResult.Detail.PRURL != "" {
		fmt.Fprintf(stdout, "  PR: %s\n", doneResult.Detail.PRURL)
	}
	if doneResult.Hint != "" {
		fmt.Fprintf(stdout, "\n  %s\n", style.Dim.Render(doneResult.Hint))
	}

	printNextHint(stdout, "Next: wl infer verify "+wantedID)

	return nil
}
