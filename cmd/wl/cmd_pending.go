package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/sdk"
	"github.com/gastownhall/wasteland/internal/style"
	"github.com/spf13/cobra"
)

type pendingSubmissionJSON struct {
	RigHandle   string `json:"rig_handle"`
	Status      string `json:"status,omitempty"`
	ClaimedBy   string `json:"claimed_by,omitempty"`
	Branch      string `json:"branch,omitempty"`
	BranchURL   string `json:"branch_url,omitempty"`
	PRURL       string `json:"pr_url,omitempty"`
	CompletedBy string `json:"completed_by,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

type pendingItemJSON struct {
	WantedID         string                  `json:"wanted_id"`
	Title            string                  `json:"title,omitempty"`
	CurrentStatus    string                  `json:"current_status,omitempty"`
	CurrentClaimedBy string                  `json:"current_claimed_by,omitempty"`
	Submissions      []pendingSubmissionJSON `json:"submissions"`
}

type pendingReportJSON struct {
	Items []pendingItemJSON `json:"items"`
}

func newPendingCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		jsonOut bool
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "pending [wanted-id]",
		Short: "Show pending upstream submissions from the Wasteland PR flow",
		Long: `Show pending upstream submissions from the Wasteland PR flow.

Without an argument, lists wanted items that currently have upstream
submissions. With a wanted ID, shows the full pending submission detail for
that item, including submitter, branch, PR URL, and evidence when available.

Examples:
  wl pending
  wl pending --json
  wl pending w-abc123
  wl pending w-abc123 --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var wantedID string
			if len(args) == 1 {
				wantedID = args[0]
			}
			return runPending(cmd, stdout, stderr, wantedID, jsonOut, limit)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum items to scan when listing all pending submissions")
	cmd.ValidArgsFunction = completeWantedIDs("")

	return cmd
}

func runPending(cmd *cobra.Command, stdout, _ io.Writer, wantedID string, jsonOut bool, limit int) error {
	wlCfg, err := resolveWasteland(cmd)
	if err != nil {
		return hintWrap(err)
	}

	client, err := newCommandClient(wlCfg, false)
	if err != nil {
		return err
	}

	var report pendingReportJSON
	if wantedID != "" {
		wantedID, err = resolveWantedArg(wlCfg, wantedID)
		if err != nil {
			return err
		}
		detail, err := client.Detail(wantedID)
		if err != nil {
			return fmt.Errorf("querying wanted item: %w", err)
		}
		if detail == nil || detail.Item == nil {
			return fmt.Errorf("wanted item %s not found", wantedID)
		}
		report.Items = append(report.Items, pendingItemJSON{
			WantedID:         detail.Item.ID,
			Title:            detail.Item.Title,
			CurrentStatus:    detail.Item.Status,
			CurrentClaimedBy: detail.Item.ClaimedBy,
			Submissions:      pendingSubmissionJSONs(detail.UpstreamPRs),
		})
	} else {
		result, err := client.Browse(commons.BrowseFilter{
			View:  "all",
			Limit: limit,
		})
		if err != nil {
			return fmt.Errorf("querying wanted board: %w", err)
		}
		report.Items = pendingReportFromBrowse(result)
	}

	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if wantedID != "" {
		renderPendingDetail(stdout, report)
		return nil
	}
	renderPendingList(stdout, report)
	return nil
}

func pendingReportFromBrowse(result *sdk.BrowseResult) []pendingItemJSON {
	if result == nil || len(result.UpstreamPending) == 0 {
		return nil
	}

	itemsByID := make(map[string]commons.WantedSummary, len(result.Items))
	for _, item := range result.Items {
		itemsByID[item.ID] = item
	}

	report := make([]pendingItemJSON, 0, len(result.UpstreamPending))
	seen := make(map[string]struct{}, len(result.UpstreamPending))
	for _, item := range result.Items {
		pending := result.UpstreamPending[item.ID]
		if len(pending) == 0 {
			continue
		}
		report = append(report, pendingItemJSON{
			WantedID:         item.ID,
			Title:            item.Title,
			CurrentStatus:    item.Status,
			CurrentClaimedBy: item.ClaimedBy,
			Submissions:      pendingSubmissionJSONs(pending),
		})
		seen[item.ID] = struct{}{}
	}

	extraIDs := make([]string, 0, len(result.UpstreamPending))
	for id := range result.UpstreamPending {
		if _, ok := seen[id]; ok {
			continue
		}
		extraIDs = append(extraIDs, id)
	}
	sort.Strings(extraIDs)
	for _, id := range extraIDs {
		item := itemsByID[id]
		report = append(report, pendingItemJSON{
			WantedID:         id,
			Title:            item.Title,
			CurrentStatus:    item.Status,
			CurrentClaimedBy: item.ClaimedBy,
			Submissions:      pendingSubmissionJSONs(result.UpstreamPending[id]),
		})
	}

	return report
}

func pendingSubmissionJSONs(items []sdk.PendingItem) []pendingSubmissionJSON {
	if len(items) == 0 {
		return nil
	}
	out := make([]pendingSubmissionJSON, 0, len(items))
	for _, item := range items {
		out = append(out, pendingSubmissionJSON{
			RigHandle:   item.RigHandle,
			Status:      item.Status,
			ClaimedBy:   item.ClaimedBy,
			Branch:      item.Branch,
			BranchURL:   item.BranchURL,
			PRURL:       item.PRURL,
			CompletedBy: item.CompletedBy,
			Evidence:    item.Evidence,
		})
	}
	return out
}

func renderPendingList(w io.Writer, report pendingReportJSON) {
	if len(report.Items) == 0 {
		fmt.Fprintln(w, "No pending upstream submissions.")
		return
	}

	fmt.Fprintf(w, "%s Pending upstream submissions (%d items)\n", style.Bold.Render("✓"), len(report.Items))
	for _, item := range report.Items {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s %s\n", style.Bold.Render(item.WantedID), item.Title)
		if item.CurrentStatus != "" {
			fmt.Fprintf(w, "  Current status: %s\n", item.CurrentStatus)
		}
		if item.CurrentClaimedBy != "" {
			fmt.Fprintf(w, "  Claimed by:     %s\n", item.CurrentClaimedBy)
		}
		fmt.Fprintf(w, "  Submissions:    %d\n", len(item.Submissions))
		for _, submission := range item.Submissions {
			fmt.Fprintf(w, "    - %s", submission.RigHandle)
			if submission.Status != "" {
				fmt.Fprintf(w, " (%s)", submission.Status)
			}
			fmt.Fprintln(w)
			if submission.PRURL != "" {
				fmt.Fprintf(w, "      PR:        %s\n", submission.PRURL)
			}
			if submission.Branch != "" {
				fmt.Fprintf(w, "      Branch:    %s\n", submission.Branch)
			}
			if submission.Evidence != "" {
				fmt.Fprintf(w, "      Evidence:  %s\n", submission.Evidence)
			}
		}
	}
}

func renderPendingDetail(w io.Writer, report pendingReportJSON) {
	if len(report.Items) == 0 {
		fmt.Fprintln(w, "No pending upstream submissions.")
		return
	}

	item := report.Items[0]
	fmt.Fprintf(w, "%s Pending submissions for %s\n", style.Bold.Render(item.WantedID), item.Title)
	if item.CurrentStatus != "" {
		fmt.Fprintf(w, "  Current status: %s\n", item.CurrentStatus)
	}
	if item.CurrentClaimedBy != "" {
		fmt.Fprintf(w, "  Claimed by:     %s\n", item.CurrentClaimedBy)
	}
	if len(item.Submissions) == 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  No pending upstream submissions.")
		return
	}

	for _, submission := range item.Submissions {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Submitter:    %s\n", submission.RigHandle)
		if submission.Status != "" {
			fmt.Fprintf(w, "  Status:       %s\n", submission.Status)
		}
		if submission.ClaimedBy != "" {
			fmt.Fprintf(w, "  Claimed by:   %s\n", submission.ClaimedBy)
		}
		if submission.Branch != "" {
			fmt.Fprintf(w, "  Branch:       %s\n", submission.Branch)
		}
		if submission.BranchURL != "" {
			fmt.Fprintf(w, "  Branch URL:   %s\n", submission.BranchURL)
		}
		if submission.PRURL != "" {
			fmt.Fprintf(w, "  PR:           %s\n", submission.PRURL)
		}
		if submission.CompletedBy != "" {
			fmt.Fprintf(w, "  Completed by: %s\n", submission.CompletedBy)
		}
		if submission.Evidence != "" {
			fmt.Fprintf(w, "  Evidence:     %s\n", submission.Evidence)
		}
	}
}
