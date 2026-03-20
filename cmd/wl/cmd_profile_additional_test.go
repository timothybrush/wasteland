package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/pile"
)

func TestTruncateField(t *testing.T) {
	if got := truncateField("short", 10); got != "short" {
		t.Fatalf("truncateField(short) = %q", got)
	}
	if got := truncateField("1234567890", 6); got != "123..." {
		t.Fatalf("truncateField(long) = %q", got)
	}
}

func TestRunProfile_PrintsProfileSections(t *testing.T) {
	withPileOverrides(
		t,
		func() *pile.Client { return nil },
		func(_ pile.RowQuerier, handle string) (*pile.Profile, error) {
			if handle != "alice" {
				t.Fatalf("handle = %q", handle)
			}
			return &pile.Profile{
				Handle:          "alice",
				DisplayName:     "Alice Rig",
				Bio:             "Builds useful things",
				Location:        "Berlin",
				Source:          "github",
				Confidence:      0.91,
				Followers:       42,
				AccountAge:      7.5,
				Quality:         4.5,
				Reliability:     4.0,
				Creativity:      3.5,
				AssessmentCount: 3,
				TotalStars:      9001,
				TotalRepos:      12,
				Languages: []pile.SkillEntry{{
					Name:        "Go",
					Quality:     5,
					Reliability: 4,
					Creativity:  3,
					Message:     "Built backend systems",
				}},
				Domains: []pile.SkillEntry{{
					Name:        "Distributed Systems",
					Quality:     4,
					Reliability: 5,
					Creativity:  3,
					Message:     "Ran production services",
				}},
				Capabilities: []pile.SkillEntry{{
					Name:        "Testing",
					Quality:     5,
					Reliability: 5,
					Creativity:  2,
					Message:     "Writes reliable test suites",
				}},
				NotableProjects: []pile.Project{{
					Name:       "wasteland",
					Stars:      123,
					Languages:  []string{"Go", "TypeScript"},
					Role:       "maintainer",
					ImpactTier: "A",
				}},
			}, nil
		},
		nil,
	)

	var stdout bytes.Buffer
	if err := runProfile(nil, &stdout, io.Discard, "alice"); err != nil {
		t.Fatalf("runProfile() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"@alice",
		"Builds useful things",
		"Value Dimensions",
		"Languages",
		"Domains",
		"Capabilities",
		"Notable Projects",
		"Assessments: 3  Total stars: 9001  Repos: 12",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q in %q", want, out)
		}
	}
}

func TestRunProfile_NotFound(t *testing.T) {
	withPileOverrides(
		t,
		func() *pile.Client { return nil },
		func(_ pile.RowQuerier, _ string) (*pile.Profile, error) { return nil, nil },
		nil,
	)

	err := runProfile(nil, io.Discard, io.Discard, "ghost")
	if err == nil || !strings.Contains(err.Error(), `profile not found for "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunProfileSearch_PrintsResultsAndEmptyState(t *testing.T) {
	withPileOverrides(
		t,
		func() *pile.Client { return nil },
		nil,
		func(_ pile.RowQuerier, query string, limit int) ([]pile.ProfileSummary, error) {
			if limit != 20 {
				t.Fatalf("limit = %d", limit)
			}
			switch query {
			case "alice":
				return []pile.ProfileSummary{
					{Handle: "alice", DisplayName: "Alice Rig"},
					{Handle: "alice-dev", DisplayName: "Alice Dev"},
				}, nil
			case "nobody":
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected query %q", query)
			}
		},
	)

	var found bytes.Buffer
	if err := runProfileSearch(nil, &found, io.Discard, "alice"); err != nil {
		t.Fatalf("runProfileSearch(found) error = %v", err)
	}
	if out := found.String(); !strings.Contains(out, "Found 2 profiles:") || !strings.Contains(out, "alice-dev") {
		t.Fatalf("found output = %q", out)
	}

	var empty bytes.Buffer
	if err := runProfileSearch(nil, &empty, io.Discard, "nobody"); err != nil {
		t.Fatalf("runProfileSearch(empty) error = %v", err)
	}
	if out := empty.String(); !strings.Contains(out, "No profiles found.") {
		t.Fatalf("empty output = %q", out)
	}
}
