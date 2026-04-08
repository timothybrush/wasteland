package main

import (
	"fmt"
	"io"

	"github.com/gastownhall/wasteland/internal/style"
	"github.com/spf13/cobra"
)

func newRejectUpstreamCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reject-upstream <wanted-id> <submitter-handle>",
		Short: "Reject a pending upstream submission by closing its PR",
		Long: `Reject a pending upstream submission by closing its upstream PR.

This does not modify the main wanted item state. It is intended for declining
an upstream submission while leaving the item itself untouched.

Examples:
  wl reject-upstream w-abc123 charlie`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRejectUpstream(cmd, stdout, stderr, args[0], args[1])
		},
	}

	cmd.ValidArgsFunction = completeUpstreamActionArgs()
	return cmd
}

func runRejectUpstream(cmd *cobra.Command, stdout, _ io.Writer, wantedID, submitterHandle string) error {
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
	if err := client.RejectUpstream(wantedID, submitterHandle); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s Rejected upstream submission for %s\n", style.Bold.Render("✓"), wantedID)
	fmt.Fprintf(stdout, "  Submitter: %s\n", submitterHandle)
	printNextHint(stdout, "Next: review remaining submissions: wl pending "+wantedID)
	return nil
}
