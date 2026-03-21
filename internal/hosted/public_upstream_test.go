package hosted

import "testing"

func TestPublicDoltHubQueryURL_UsesCanonicalPublicRepo(t *testing.T) {
	if PublicUpstreamOrg != "hop" {
		t.Fatalf("PublicUpstreamOrg = %q", PublicUpstreamOrg)
	}
	if PublicUpstreamDB != "wl-commons" {
		t.Fatalf("PublicUpstreamDB = %q", PublicUpstreamDB)
	}
	if PublicUpstream != "hop/wl-commons" {
		t.Fatalf("PublicUpstream = %q", PublicUpstream)
	}
	if got := PublicDoltHubQueryURL("SELECT 1"); got != "https://www.dolthub.com/api/v1alpha1/hop/wl-commons/main?q=SELECT%201" {
		t.Fatalf("PublicDoltHubQueryURL() = %q", got)
	}
}
