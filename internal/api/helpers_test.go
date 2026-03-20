package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gastownhall/wasteland/internal/commons"
)

func TestParseQueryFilter_OmittedViewStaysEmpty(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/api/wanted", nil)
	filter := parseQueryFilter(req)

	if filter.View != "" {
		t.Fatalf("View = %q, want empty", filter.View)
	}
	if filter.Priority != -1 {
		t.Fatalf("Priority = %d, want -1", filter.Priority)
	}
	if filter.Sort != commons.SortPriority {
		t.Fatalf("Sort = %v, want %v", filter.Sort, commons.SortPriority)
	}
}

func TestParseQueryFilter_RespectsExplicitView(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/api/wanted?view=upstream&sort=alpha&priority=3", nil)
	filter := parseQueryFilter(req)

	if filter.View != "upstream" {
		t.Fatalf("View = %q, want upstream", filter.View)
	}
	if filter.Priority != 3 {
		t.Fatalf("Priority = %d, want 3", filter.Priority)
	}
	if filter.Sort != commons.SortAlpha {
		t.Fatalf("Sort = %v, want %v", filter.Sort, commons.SortAlpha)
	}
}
