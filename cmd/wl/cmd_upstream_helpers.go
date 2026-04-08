package main

import "github.com/spf13/cobra"

func completeUpstreamActionArgs() func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return completeWantedIDs("")(cmd, args, toComplete)
		case 1:
			return completeUpstreamSubmitterHandles()(cmd, args, toComplete)
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
}
