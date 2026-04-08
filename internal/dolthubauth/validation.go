package dolthubauth

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gastownhall/wasteland/internal/federation"
)

var metadataSlugRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func validateMetadata(meta UserMetadata) error {
	if err := validateSlug("rig_handle", meta.RigHandle); err != nil {
		return err
	}
	if len(meta.Wastelands) == 0 {
		return fmt.Errorf("at least one wasteland is required")
	}
	for _, wl := range meta.Wastelands {
		if err := validateWastelandConfig(wl); err != nil {
			return err
		}
	}
	return nil
}

func validateWastelandConfig(wl WastelandConfig) error {
	if err := validateSlug("fork_org", wl.ForkOrg); err != nil {
		return err
	}
	if err := validateSlug("fork_db", wl.ForkDB); err != nil {
		return err
	}
	if err := validateUpstream(wl.Upstream); err != nil {
		return err
	}
	return validateMode(wl.Mode)
}

func normalizeMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return federation.ModePR
	}
	return mode
}

func validateMode(mode string) error {
	switch normalizeMode(mode) {
	case federation.ModePR, federation.ModeWildWest:
		return nil
	default:
		return fmt.Errorf("mode must be %q or %q", federation.ModePR, federation.ModeWildWest)
	}
}

func validateUpstream(value string) error {
	org, db, ok := strings.Cut(value, "/")
	if !ok || org == "" || db == "" {
		return fmt.Errorf("upstream must be in org/db format")
	}
	if err := validateSlug("upstream org", org); err != nil {
		return err
	}
	return validateSlug("upstream db", db)
}

func validateSlug(field, value string) error {
	if !metadataSlugRe.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("%s must be 1-64 alphanumeric characters, hyphens, or underscores", field)
	}
	return nil
}
