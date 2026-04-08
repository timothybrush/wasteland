package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newCloseUpstreamCmd(stdout, stderr io.Writer) *cobra.Command {
	var noPush bool

	cmd := &cobra.Command{
		Use:   "close-upstream <wanted-id> <submitter-handle>",
		Short: "Adopt a pending upstream submission without issuing a stamp",
		Long: `Close a pending upstream submission by adopting its completion without
issuing a stamp, then best-effort closing the upstream PR.

Examples:
  wl close-upstream w-abc123 charlie
  wl close-upstream w-abc123 charlie --no-push`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloseUpstream(cmd, stdout, stderr, args[0], args[1], noPush)
		},
	}

	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	cmd.ValidArgsFunction = completeUpstreamActionArgs()
	return cmd
}

func runCloseUpstream(cmd *cobra.Command, stdout, _ io.Writer, wantedID, submitterHandle string, noPush bool) error {
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
	result, err := client.CloseUpstream(wantedID, submitterHandle)
	if err != nil {
		return err
	}

	renderMutationResult(stdout, "Closed upstream submission for", wantedID, result, "Submitter: "+submitterHandle)
	printNextHint(stdout, fmt.Sprintf("Next: item completed without stamp. View: wl status %s", wantedID))
	return nil
}
