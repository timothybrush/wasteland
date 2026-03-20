package hosted

import "testing"

func TestValidateSlug(t *testing.T) {
	valid := []string{"alice", "alice-dev", "org_name", "A1", "a"}
	for _, s := range valid {
		if err := validateSlug("test", s); err != nil {
			t.Errorf("validateSlug(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{"", "-starts-with-dash", " spaces", "has/slash", "a b", "tab\there", string(make([]byte, 65))}
	for _, s := range invalid {
		if err := validateSlug("test", s); err == nil {
			t.Errorf("validateSlug(%q) = nil, want error", s)
		}
	}
}

func TestValidateUpstream(t *testing.T) {
	valid := []string{"hop/wl-commons", "alice/my_db", "org/db"}
	for _, s := range valid {
		if err := validateUpstream(s); err != nil {
			t.Errorf("validateUpstream(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{"", "noslash", "/db", "org/", "org/db/extra", "org/ db", " /db"}
	for _, s := range invalid {
		if err := validateUpstream(s); err == nil {
			t.Errorf("validateUpstream(%q) = nil, want error", s)
		}
	}
}

func TestValidateConnectFields(t *testing.T) {
	if err := validateConnectFields("alice", "alice-org", "wl-commons", "hop/wl-commons"); err != nil {
		t.Fatalf("validateConnectFields() error = %v", err)
	}
	if err := validateConnectFields("bad handle", "alice-org", "wl-commons", "hop/wl-commons"); err == nil {
		t.Fatal("expected invalid rig_handle error")
	}
	if err := validateConnectFields("alice", "alice-org", "bad/db", "hop/wl-commons"); err == nil {
		t.Fatal("expected invalid fork_db error")
	}
}

func TestValidateJoinFields(t *testing.T) {
	if err := validateJoinFields("alice-org", "wl-commons", "hop/wl-commons"); err != nil {
		t.Fatalf("validateJoinFields() error = %v", err)
	}
	if err := validateJoinFields("bad org", "wl-commons", "hop/wl-commons"); err == nil {
		t.Fatal("expected invalid fork_org error")
	}
	if err := validateJoinFields("alice-org", "wl-commons", "bad-upstream"); err == nil {
		t.Fatal("expected invalid upstream error")
	}
}
