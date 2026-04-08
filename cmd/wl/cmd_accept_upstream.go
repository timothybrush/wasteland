package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/gastownhall/wasteland/internal/sdk"
	"github.com/spf13/cobra"
)

func newAcceptUpstreamCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		quality     int
		reliability int
		severity    string
		skills      string
		message     string
		noPush      bool
	)

	cmd := &cobra.Command{
		Use:   "accept-upstream <wanted-id> <submitter-handle>",
		Short: "Accept a pending upstream submission and issue a stamp",
		Long: `Accept a pending upstream submission from the Wasteland PR flow.

This adopts the submitter's upstream PR into the main wanted item, creates a
completion record, and issues a stamp. The submitter must currently have an
in-review upstream submission for the wanted item.

Examples:
  wl accept-upstream w-abc123 charlie --quality 4
  wl accept-upstream w-abc123 charlie --quality 5 --reliability 4 --severity branch
  wl accept-upstream w-abc123 charlie --quality 3 --skills "go,federation" --message "solid work"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAcceptUpstream(cmd, stdout, stderr, args[0], args[1], quality, reliability, severity, skills, message, noPush)
		},
	}

	cmd.Flags().IntVar(&quality, "quality", 0, "Quality rating 1-5 (required)")
	cmd.Flags().IntVar(&reliability, "reliability", 0, "Reliability rating 1-5 (defaults to quality)")
	cmd.Flags().StringVar(&severity, "severity", "leaf", "Severity: leaf, branch, root")
	cmd.Flags().StringVar(&skills, "skills", "", "Comma-separated skill tags")
	cmd.Flags().StringVar(&message, "message", "", "Freeform message")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing to remotes (offline work)")
	_ = cmd.MarkFlagRequired("quality")
	cmd.ValidArgsFunction = completeUpstreamActionArgs()
	_ = cmd.RegisterFlagCompletionFunc("severity", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"leaf", "branch", "root"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

func runAcceptUpstream(cmd *cobra.Command, stdout, _ io.Writer, wantedID, submitterHandle string, quality, reliability int, severity, skills, message string, noPush bool) error {
	if reliability == 0 {
		reliability = quality
	}
	if err := validateAcceptInputs(quality, reliability, severity); err != nil {
		return err
	}

	var skillTags []string
	if skills != "" {
		for _, s := range strings.Split(skills, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				skillTags = append(skillTags, s)
			}
		}
	}

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

	result, err := client.AcceptUpstream(wantedID, submitterHandle, sdk.AcceptInput{
		Quality:     quality,
		Reliability: reliability,
		Severity:    severity,
		SkillTags:   skillTags,
		Message:     message,
	})
	if err != nil {
		return err
	}

	extras := []string{
		"Submitter: " + submitterHandle,
		fmt.Sprintf("Quality: %d, Reliability: %d", quality, reliability),
		"Severity: " + severity,
	}
	if len(skillTags) > 0 {
		extras = append(extras, "Skills: "+strings.Join(skillTags, ", "))
	}
	if message != "" {
		extras = append(extras, "Message: "+message)
	}

	renderMutationResult(stdout, "Accepted upstream submission for", wantedID, result, extras...)
	printNextHint(stdout, "Next: view the adopted item: wl status "+wantedID)

	return nil
}
