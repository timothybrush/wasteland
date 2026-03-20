package main

import (
	"io"

	"github.com/spf13/cobra"
)

func newRejectCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		reason string
		noPush bool
	)

	cmd := &cobra.Command{
		Use:   "reject <wanted-id>",
		Short: "Reject a completed wanted item back to claimed",
		Long: `Reject a completed wanted item, reverting it from 'in_review' to 'claimed'.

The item must be in 'in_review' status. Only the poster can reject.
The completion record is deleted so the claimer can re-submit.

In wild-west mode the commit is auto-pushed to upstream and origin.
Use --no-push to skip pushing (offline work).

Examples:
  wl reject w-abc123
  wl reject w-abc123 --reason "tests failing"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReject(cmd, stdout, stderr, args[0], reason, noPush)
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Reason for rejection (included in commit message)")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	cmd.ValidArgsFunction = completeWantedIDs("in_review")

	return cmd
}

func runReject(cmd *cobra.Command, stdout, _ io.Writer, wantedID, reason string, noPush bool) error {
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

	result, err := client.Reject(wantedID, reason)
	if err != nil {
		return err
	}

	var extras []string
	if reason != "" {
		extras = append(extras, "Reason: "+reason)
	}

	renderMutationResult(stdout, "Rejected", wantedID, result, extras...)
	printNextHint(stdout, "Next: claimer can fix and resubmit: wl done "+wantedID+" --evidence <url>")

	return nil
}
