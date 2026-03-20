package main

import (
	"fmt"
	"io"

	"github.com/gastownhall/wasteland/internal/style"
	"github.com/spf13/cobra"
)

func newClaimCmd(stdout, stderr io.Writer) *cobra.Command {
	var noPush bool

	cmd := &cobra.Command{
		Use:   "claim <wanted-id>",
		Short: "Claim a wanted item",
		Long: `Claim a wanted item on the shared wanted board.

Updates the wanted row: claimed_by=<your rig handle>, status='claimed'.
The item must exist and have status='open'.

In wild-west mode the commit is auto-pushed to upstream and origin.
Use --no-push to skip pushing (offline work).

Examples:
  wl claim w-abc123
  wl claim w-abc123 --no-push`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClaim(cmd, stdout, stderr, args[0], noPush)
		},
	}

	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	cmd.ValidArgsFunction = completeWantedIDs("open")

	return cmd
}

func runClaim(cmd *cobra.Command, stdout, _ io.Writer, wantedID string, noPush bool) error {
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

	result, err := client.Claim(wantedID)
	if err != nil {
		return err
	}

	renderMutationResult(stdout, "Claimed", wantedID, result,
		"Claimed by: "+wlCfg.RigHandle)

	hint := "Next: do the work, then: wl done " + wantedID + " --evidence <url>"
	printNextHint(stdout, hint)

	return nil
}

func printNextHint(w io.Writer, hint string) {
	fmt.Fprintf(w, "\n  %s\n", style.Dim.Render(hint))
}
