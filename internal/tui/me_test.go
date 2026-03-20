package tui

import (
	"fmt"
	"strings"
	"testing"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/gastownhall/wasteland/internal/commons"
)

func dashboardFixture() *commons.DashboardData {
	return &commons.DashboardData{
		Claimed: []commons.WantedSummary{
			{ID: "w-1", Title: "Claimed item", Project: "hop", Priority: 1, Status: "claimed"},
		},
		InReview: []commons.WantedSummary{
			{ID: "w-2", Title: "Review item", Project: "hop", Priority: 2, Status: "in_review"},
		},
		Completed: []commons.WantedSummary{
			{ID: "w-3", Title: "Completed item", Project: "hop", Priority: 3, Status: "completed"},
		},
	}
}

func TestMeModel_SetDataNavigationAndSelection(t *testing.T) {
	m := newMeModel()
	m.setSize(100, 30)
	if m.width != 100 || m.height != 30 {
		t.Fatalf("setSize() = %dx%d, want 100x30", m.width, m.height)
	}

	m.cursor = 9
	m.setData(meDataMsg{data: dashboardFixture()})
	if m.loading {
		t.Fatal("setData() should clear loading")
	}
	if got := m.totalItems(); got != 3 {
		t.Fatalf("totalItems() = %d, want 3", got)
	}
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want clamped to 2", m.cursor)
	}
	if item := m.selectedItem(); item == nil || item.ID != "w-3" {
		t.Fatalf("selectedItem() = %+v, want w-3", item)
	}

	m.cursor = 0
	if item := m.selectedItem(); item == nil || item.ID != "w-1" {
		t.Fatalf("selectedItem() at cursor 0 = %+v, want w-1", item)
	}

	m2, _ := m.update(keyMsg("j"))
	if m2.cursor != 1 {
		t.Fatalf("cursor after j = %d, want 1", m2.cursor)
	}

	_, cmd := m2.update(bubbletea.KeyMsg{Type: bubbletea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return navigate cmd")
	}
	msg := cmd()
	nav, ok := msg.(navigateMsg)
	if !ok {
		t.Fatalf("enter cmd = %T, want navigateMsg", msg)
	}
	if nav.view != viewDetail || nav.wantedID != "w-2" {
		t.Fatalf("navigateMsg = %+v, want detail/w-2", nav)
	}

	_, cmd = m2.update(bubbletea.KeyMsg{Type: bubbletea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should return navigate cmd")
	}
	msg = cmd()
	nav, ok = msg.(navigateMsg)
	if !ok || nav.view != viewBrowse {
		t.Fatalf("esc cmd = %+v, want browse navigateMsg", msg)
	}

	_, cmd = m2.update(keyMsg("S"))
	if cmd == nil {
		t.Fatal("S should return navigate cmd")
	}
	msg = cmd()
	nav, ok = msg.(navigateMsg)
	if !ok || nav.view != viewSettings {
		t.Fatalf("settings cmd = %+v, want settings navigateMsg", msg)
	}
}

func TestMeModel_ViewStates(t *testing.T) {
	m := newMeModel()
	if got := m.view(); !strings.Contains(got, "Loading...") {
		t.Fatalf("loading view = %q, want Loading...", got)
	}

	m.loading = false
	m.err = fmt.Errorf("dashboard failed")
	if got := m.view(); !strings.Contains(got, "dashboard failed") {
		t.Fatalf("error view = %q, want dashboard failed", got)
	}

	m.err = nil
	if got := m.view(); !strings.Contains(got, "No data.") {
		t.Fatalf("nil-data view = %q, want No data.", got)
	}

	m.data = &commons.DashboardData{}
	if got := m.view(); !strings.Contains(got, "No items to show.") {
		t.Fatalf("empty-data view = %q, want no items", got)
	}

	m.width = 120
	m.data = dashboardFixture()
	if got := m.view(); !strings.Contains(got, "My Dashboard") || !strings.Contains(got, "Claimed item") || !strings.Contains(got, "Recent Completions") {
		t.Fatalf("dashboard view missing expected content:\n%s", got)
	}
}
