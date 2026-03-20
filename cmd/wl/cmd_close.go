package main

import (
	"io"

	"github.com/spf13/cobra"
)

func newCloseCmd(stdout, stderr io.Writer) *cobra.Command {
	var noPush bool

	cmd := &cobra.Command{
		Use:   "close <wanted-id>",
		Short: "Close an in_review item as completed (no stamp)",
		Long: `Close an in_review wanted item by marking it as completed without issuing
a reputation stamp. This is housekeeping for solo maintainers who posted,
claimed, and completed their own work.

The item must be in 'in_review' status and only the poster can close it.

In wild-west mode the commit is auto-pushed to upstream and origin.
Use --no-push to skip pushing (offline work).

Examples:
  wl close w-abc123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClose(cmd, stdout, stderr, args[0], noPush)
		},
	}

	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	cmd.ValidArgsFunction = completeWantedIDs("in_review")

	return cmd
}

func runClose(cmd *cobra.Command, stdout, _ io.Writer, wantedID string, noPush bool) error {
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

	result, err := client.Close(wantedID)
	if err != nil {
		return err
	}

	renderMutationResult(stdout, "Closed", wantedID, result)
	printNextHint(stdout, "Next: item completed. View: wl status "+wantedID)

	return nil
}
