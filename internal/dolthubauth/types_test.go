package dolthubauth

import "testing"

func TestUserMetadataMergedWithPreservesExistingWastelands(t *testing.T) {
	base := UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{Upstream: "hop/wl-commons", ForkOrg: "alice", ForkDB: "wl-commons", Mode: "pr", Signing: true},
			{Upstream: "hop/wl-other", ForkOrg: "alice", ForkDB: "wl-other", Mode: "pr", Signing: false},
		},
	}
	update := UserMetadata{
		RigHandle: "alice-renamed",
		Wastelands: []WastelandConfig{
			{Upstream: "hop/wl-commons", ForkOrg: "alice", ForkDB: "wl-commons-new", Mode: "wild-west", Signing: true},
		},
	}

	merged := base.MergedWith(update)
	if merged.RigHandle != "alice-renamed" {
		t.Fatalf("RigHandle = %q, want alice-renamed", merged.RigHandle)
	}
	if len(merged.Wastelands) != 2 {
		t.Fatalf("len(Wastelands) = %d, want 2", len(merged.Wastelands))
	}
	if got := merged.FindWasteland("hop/wl-commons"); got == nil || got.ForkDB != "wl-commons-new" || got.Mode != "wild-west" {
		t.Fatalf("updated wasteland = %+v", got)
	}
	if got := merged.FindWasteland("hop/wl-other"); got == nil || got.ForkDB != "wl-other" || got.Signing != false {
		t.Fatalf("preserved wasteland = %+v", got)
	}
}
