package main

import (
	"io"

	"github.com/spf13/cobra"
)

func newUnclaimCmd(stdout, stderr io.Writer) *cobra.Command {
	var noPush bool

	cmd := &cobra.Command{
		Use:   "unclaim <wanted-id>",
		Short: "Release a claimed wanted item back to open",
		Long: `Release a claimed wanted item, reverting it from 'claimed' to 'open'.

The item must be in 'claimed' status. Only the claimer or the poster can unclaim.

In wild-west mode the commit is auto-pushed to upstream and origin.
Use --no-push to skip pushing (offline work).

Examples:
  wl unclaim w-abc123
  wl unclaim w-abc123 --no-push`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUnclaim(cmd, stdout, stderr, args[0], noPush)
		},
	}

	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	cmd.ValidArgsFunction = completeWantedIDs("claimed")

	return cmd
}

func runUnclaim(cmd *cobra.Command, stdout, _ io.Writer, wantedID string, noPush bool) error {
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

	result, err := client.Unclaim(wantedID)
	if err != nil {
		return err
	}

	renderMutationResult(stdout, "Unclaimed", wantedID, result)
	printNextHint(stdout, "Next: item is back on the board. Browse: wl browse")

	return nil
}
