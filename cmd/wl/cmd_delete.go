package main

import (
	"fmt"
	"io"

	"github.com/gastownhall/wasteland/internal/style"
	"github.com/spf13/cobra"
)

func newDeleteCmd(stdout, stderr io.Writer) *cobra.Command {
	var noPush bool

	cmd := &cobra.Command{
		Use:   "delete <wanted-id>",
		Short: "Withdraw a wanted item",
		Long: `Withdraw a wanted item by setting its status to 'withdrawn'.

Only items with status 'open' can be withdrawn — claimed or in-review items
have active workers. The row stays in the table for audit trail.

In wild-west mode any joined rig can delete.

In wild-west mode the commit is auto-pushed to upstream and origin.
Use --no-push to skip pushing (offline work).

Examples:
  wl delete w-abc123
  wl delete w-abc123 --no-push`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, stdout, stderr, args[0], noPush)
		},
	}

	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	cmd.ValidArgsFunction = completeWantedIDs("open")

	return cmd
}

func runDelete(cmd *cobra.Command, stdout, _ io.Writer, wantedID string, noPush bool) error {
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

	result, err := client.Delete(wantedID)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s Withdrawn %s\n", style.Bold.Render("✓"), wantedID)
	if result.Detail != nil && result.Detail.Item != nil {
		fmt.Fprintf(stdout, "  Status: %s\n", result.Detail.Item.Status)
	} else {
		fmt.Fprintf(stdout, "  Status: withdrawn\n")
	}
	if result.Branch != "" {
		fmt.Fprintf(stdout, "  Branch: %s\n", result.Branch)
	}
	if result.Hint != "" {
		fmt.Fprintf(stdout, "\n  %s\n", style.Dim.Render(result.Hint))
	}

	printNextHint(stdout, "Next: wl browse to see the board")

	return nil
}
