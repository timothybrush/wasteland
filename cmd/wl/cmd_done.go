package main

import (
	"io"

	"github.com/spf13/cobra"
)

func newDoneCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		evidence string
		noPush   bool
	)

	cmd := &cobra.Command{
		Use:   "done <wanted-id>",
		Short: "Submit completion evidence for a wanted item",
		Long: `Submit completion evidence for a claimed wanted item.

Inserts a completion record and updates the wanted item status to 'in_review'.
The item must be claimed by your rig.

The --evidence flag provides the evidence URL (PR link, commit hash, etc.).

A completion ID is generated as c-<hash> where hash is derived from the
wanted ID, rig handle, and timestamp.

In wild-west mode the commit is auto-pushed to upstream and origin.
Use --no-push to skip pushing (offline work).

Examples:
  wl done w-abc123 --evidence 'https://github.com/org/repo/pull/123'
  wl done w-abc123 --evidence 'commit abc123def'
  wl done w-abc123 --evidence 'commit abc123def' --no-push`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDone(cmd, stdout, stderr, args[0], evidence, noPush)
		},
	}

	cmd.Flags().StringVar(&evidence, "evidence", "", "Evidence URL or description (required)")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	_ = cmd.MarkFlagRequired("evidence")
	cmd.ValidArgsFunction = completeWantedIDs("claimed")

	return cmd
}

func runDone(cmd *cobra.Command, stdout, _ io.Writer, wantedID, evidence string, noPush bool) error {
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

	result, err := client.Done(wantedID, evidence)
	if err != nil {
		return err
	}

	renderMutationResult(stdout, "Completion submitted for", wantedID, result,
		"Completed by: "+wlCfg.RigHandle,
		"Evidence: "+evidence)
	printNextHint(stdout, "Next: wait for review. Check: wl status "+wantedID)

	return nil
}
