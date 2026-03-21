package hosted

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	// PublicUpstreamOrg defines the anonymous hosted board owner that backs
	// logged-out browse, detail, and scoreboard reads.
	PublicUpstreamOrg = "hop"
	// PublicUpstreamDB defines the anonymous hosted board name that backs
	// logged-out browse, detail, and scoreboard reads.
	PublicUpstreamDB = "wl-commons"
	// PublicUpstream is the canonical anonymous hosted board identity.
	PublicUpstream = PublicUpstreamOrg + "/" + PublicUpstreamDB
)

// PublicDoltHubQueryURL builds the DoltHub SQL API URL for the canonical
// public hosted upstream.
func PublicDoltHubQueryURL(query string) string {
	escaped := strings.ReplaceAll(url.QueryEscape(query), "+", "%20")
	return fmt.Sprintf(
		"https://www.dolthub.com/api/v1alpha1/%s/%s/main?q=%s",
		PublicUpstreamOrg,
		PublicUpstreamDB,
		escaped,
	)
}
