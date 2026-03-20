package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/gastownhall/wasteland/internal/sdk"
	"github.com/gastownhall/wasteland/internal/style"
	"github.com/spf13/cobra"
)

func newStatusCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:     "status <wanted-id>",
		Aliases: []string{"show"},
		Short:   "Show detailed status for a wanted item",
		Long: `Show the full lifecycle status of a wanted item.

Displays all fields including description, timestamps, and conditionally
shows completion and stamp details based on the item's current state.

Examples:
  wl status w-abc123`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWantedIDs(""),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, stdout, stderr, args[0])
		},
	}
}

func runStatus(cmd *cobra.Command, stdout, _ io.Writer, wantedID string) error {
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

	detail, err := client.Detail(wantedID)
	if err != nil {
		return fmt.Errorf("querying wanted item: %w", err)
	}
	if detail.Item == nil {
		return fmt.Errorf("wanted item %s not found", wantedID)
	}

	renderDetailStatus(stdout, detail)
	return nil
}

// renderDetailStatus writes the formatted status output from an SDK DetailResult.
func renderDetailStatus(w io.Writer, r *sdk.DetailResult) {
	item := r.Item

	// Header
	fmt.Fprintf(w, "%s\n", style.Bold.Render(fmt.Sprintf("%s: %s", item.ID, item.Title)))
	fmt.Fprintln(w)

	// Status with color
	fmt.Fprintf(w, "  Status:      %s\n", colorizeStatus(item.Status))

	// Type/Priority line
	typePri := "  "
	if item.Type != "" {
		typePri += fmt.Sprintf("Type:        %-14s", item.Type)
	}
	typePri += fmt.Sprintf("Priority: P%d", item.Priority)
	fmt.Fprintln(w, typePri)

	// Project/Effort line
	projEffort := "  "
	if item.Project != "" {
		projEffort += fmt.Sprintf("Project:     %-14s", item.Project)
	}
	projEffort += fmt.Sprintf("Effort:   %s", item.EffortLevel)
	fmt.Fprintln(w, projEffort)

	// Posted by
	if item.PostedBy != "" {
		fmt.Fprintf(w, "  Posted by:   %s\n", item.PostedBy)
	}

	// Tags
	if len(item.Tags) > 0 {
		fmt.Fprintf(w, "  Tags:        %s\n", strings.Join(item.Tags, ", "))
	}

	// Timestamps
	if item.CreatedAt != "" {
		fmt.Fprintf(w, "  Created:     %s\n", item.CreatedAt)
	}
	if item.UpdatedAt != "" {
		fmt.Fprintf(w, "  Updated:     %s\n", item.UpdatedAt)
	}

	// Description
	if item.Description != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Description:")
		fmt.Fprintf(w, "    %s\n", item.Description)
	}

	// Claimed by
	if item.ClaimedBy != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Claimed by:  %s\n", item.ClaimedBy)
	}

	// Branch info (PR mode)
	if r.Branch != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Branch:      %s\n", r.Branch)
		if r.BranchURL != "" {
			fmt.Fprintf(w, "  Branch URL:  %s\n", r.BranchURL)
		}
		if r.Delta != "" {
			fmt.Fprintf(w, "  Delta:       %s\n", r.Delta)
		}
		if r.MainStatus != "" {
			fmt.Fprintf(w, "  Main status: %s\n", r.MainStatus)
		}
		if r.PRURL != "" {
			fmt.Fprintf(w, "  PR:          %s\n", r.PRURL)
		}
	}

	// Completion
	if r.Completion != nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Completion:  %s\n", r.Completion.ID)
		if r.Completion.Evidence != "" {
			fmt.Fprintf(w, "    Evidence:    %s\n", r.Completion.Evidence)
		}
		fmt.Fprintf(w, "    Completed by: %s\n", r.Completion.CompletedBy)
	}

	// Stamp
	if r.Stamp != nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Stamp:       %s\n", r.Stamp.ID)
		fmt.Fprintf(w, "    Quality: %d  Reliability: %d  Severity: %s\n",
			r.Stamp.Quality, r.Stamp.Reliability, r.Stamp.Severity)
		if len(r.Stamp.SkillTags) > 0 {
			fmt.Fprintf(w, "    Skills:      %s\n", strings.Join(r.Stamp.SkillTags, ", "))
		}
		if r.Stamp.Author != "" {
			fmt.Fprintf(w, "    Accepted by: %s\n", r.Stamp.Author)
		}
		if r.Stamp.Message != "" {
			fmt.Fprintf(w, "    Message:     %s\n", r.Stamp.Message)
		}
	}
}

func colorizeStatus(status string) string {
	switch status {
	case "completed":
		return style.Success.Render(status)
	case "in_review", "claimed":
		return style.Warning.Render(status)
	case "withdrawn":
		return style.Dim.Render(status)
	default:
		return status
	}
}
