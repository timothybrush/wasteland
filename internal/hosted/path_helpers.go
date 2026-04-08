package hosted

import "strings"

// extractWantedIDFromBranch parses a branch name like "wl/{rig}/{wantedID}"
// and returns the wanted ID, or the raw branch name as fallback.
func extractWantedIDFromBranch(branch string) string {
	parts := strings.SplitN(branch, "/", 3)
	if len(parts) == 3 && parts[0] == "wl" {
		return parts[2]
	}
	return branch
}

// extractPRID extracts the pull request ID from a DoltHub PR URL like
// https://www.dolthub.com/repositories/org/db/pulls/123.
func extractPRID(prURL string) string {
	idx := strings.LastIndex(prURL, "/pulls/")
	if idx < 0 {
		return ""
	}
	return prURL[idx+len("/pulls/"):]
}
