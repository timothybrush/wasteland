package sdk

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gastownhall/wasteland/internal/commons"
)

type queryOnlyDB struct {
	queryFunc func(sql, ref string) (string, error)
}

func (db *queryOnlyDB) Query(sql, ref string) (string, error) {
	if db.queryFunc != nil {
		return db.queryFunc(sql, ref)
	}
	return "", nil
}

func (db *queryOnlyDB) Exec(string, string, bool, ...string) error { return nil }
func (db *queryOnlyDB) Branches(string) ([]string, error)          { return nil, nil }
func (db *queryOnlyDB) DeleteBranch(string) error                  { return nil }
func (db *queryOnlyDB) PushBranch(string, io.Writer) error         { return nil }
func (db *queryOnlyDB) PushMain(io.Writer) error                   { return nil }
func (db *queryOnlyDB) Sync() error                                { return nil }
func (db *queryOnlyDB) MergeBranch(string) error                   { return nil }
func (db *queryOnlyDB) DeleteRemoteBranch(string) error            { return nil }
func (db *queryOnlyDB) PushWithSync(io.Writer) error               { return nil }
func (db *queryOnlyDB) CanWildWest() error                         { return nil }

func TestWithRigHandleReturnsShallowCopy(t *testing.T) {
	db := newFakeDB()
	c := New(ClientConfig{
		DB:               db,
		RigHandle:        "alice",
		Mode:             "pr",
		Signing:          true,
		HopURI:           "hop://alice",
		CloseUpstreamPR:  func(string) error { return nil },
		LoadPendingItem:  func(string, PendingItem) (*commons.WantedItem, error) { return nil, nil },
		ListPendingItems: pendingItems(nil),
	})

	clone := c.WithRigHandle("bob")
	if clone == c {
		t.Fatal("WithRigHandle() should return a new client pointer")
	}
	if clone.RigHandle() != "bob" {
		t.Fatalf("RigHandle() = %q, want bob", clone.RigHandle())
	}
	if clone.Mode() != c.Mode() {
		t.Fatalf("Mode() = %q, want %q", clone.Mode(), c.Mode())
	}
	if clone.db != c.db || clone.CloseUpstreamPR == nil || clone.LoadPendingItem == nil || clone.ListPendingItems == nil {
		t.Fatal("WithRigHandle() should preserve DB and callbacks")
	}
	if c.RigHandle() != "alice" {
		t.Fatalf("original RigHandle() = %q, want alice", c.RigHandle())
	}
}

func TestDetail_UsesSingleJoinedQuery(t *testing.T) {
	var calls int
	db := &queryOnlyDB{
		queryFunc: func(sql, ref string) (string, error) {
			calls++
			if ref != "" {
				t.Fatalf("unexpected ref = %q", ref)
			}
			if strings.Contains(sql, "FROM completions") || strings.Contains(sql, "FROM stamps") {
				t.Fatalf("unexpected separate detail query: %s", sql)
			}
			if !strings.Contains(sql, "LEFT JOIN completions") || !strings.Contains(sql, "LEFT JOIN stamps") {
				t.Fatalf("expected joined detail query, got %s", sql)
			}
			return strings.Join([]string{
				"id,title,description,project,type,priority,tags,posted_by,claimed_by,status,effort_level,created_at,updated_at,completion_id,completion_wanted_id,completed_by,evidence,completion_stamp_id,validated_by,stamp_record_id,stamp_author,stamp_subject,stamp_valence,stamp_severity,stamp_context_id,stamp_context_type,stamp_skill_tags,stamp_message",
				`w-1,Fix hosted detail,Trace the detail path,hop,bug,1,"[""go"",""perf""]",alice,bob,in_review,medium,2026-03-20,2026-03-21,c-1,w-1,bob,https://example.com/proof,s-1,alice,s-1,alice,bob,"{""quality"":4,""reliability"":5}",medium,w-1,completion,"[""go"",""perf""]",Looks good`,
				"",
			}, "\n"), nil
		},
	}

	c := New(ClientConfig{DB: db, RigHandle: "alice", Mode: "wild-west"})
	result, err := c.Detail("w-1")
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if result.Completion == nil || result.Completion.ID != "c-1" {
		t.Fatalf("completion = %+v, want joined completion", result.Completion)
	}
	if result.Stamp == nil || result.Stamp.ID != "s-1" {
		t.Fatalf("stamp = %+v, want joined stamp", result.Stamp)
	}
}

func TestBrowse_BranchOnlyPendingUsesItemLoader(t *testing.T) {
	db := newFakeDB()
	var itemLoads, detailLoads int
	c := New(ClientConfig{
		DB:        db,
		RigHandle: "alice",
		Mode:      "pr",
		LoadPendingItem: func(wantedID string, _ PendingItem) (*commons.WantedItem, error) {
			itemLoads++
			return &commons.WantedItem{
				ID:          wantedID,
				Title:       "Pending branch-only item",
				Project:     "hop",
				Type:        "docs",
				Priority:    1,
				PostedBy:    "alice",
				Status:      "open",
				EffortLevel: "small",
			}, nil
		},
		LoadPendingDetail: func(string, PendingItem) (*commons.WantedItem, *commons.CompletionRecord, *commons.Stamp, error) {
			detailLoads++
			return nil, nil, nil, fmt.Errorf("should not be called")
		},
		ListPendingItems: pendingItems(map[string][]PendingItem{
			"w-new": {{
				RigHandle: "alice",
				Status:    "open",
				Branch:    "wl/alice/w-new",
				ForkOwner: "alice",
			}},
		}),
	})

	result, err := c.Browse(commons.BrowseFilter{View: "mine", Priority: -1})
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "w-new" {
		t.Fatalf("result.Items = %+v, want branch-only pending item", result.Items)
	}
	if itemLoads != 1 {
		t.Fatalf("itemLoads = %d, want 1", itemLoads)
	}
	if detailLoads != 0 {
		t.Fatalf("detailLoads = %d, want 0", detailLoads)
	}
}

func TestPostAndUpdate(t *testing.T) {
	db := newFakeDB()
	c := New(ClientConfig{DB: db, RigHandle: "alice", Mode: "wild-west"})

	posted, err := c.Post(PostInput{
		Title:       "Fix hosted login",
		Description: "Stop stale auth state from leaking into hosted mode",
		Project:     "hop",
		Type:        "bug",
		Priority:    1,
		EffortLevel: "small",
		Tags:        []string{"go", "auth"},
	})
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if posted.Detail == nil || posted.Detail.Item == nil {
		t.Fatal("Post() should return detail with item")
	}
	if !strings.HasPrefix(posted.Detail.Item.ID, "w-") {
		t.Fatalf("post ID = %q, want w- prefix", posted.Detail.Item.ID)
	}
	if posted.Detail.Item.PostedBy != "alice" {
		t.Fatalf("posted_by = %q, want alice", posted.Detail.Item.PostedBy)
	}
	if db.pushCalls != 1 {
		t.Fatalf("pushCalls = %d, want 1 after post", db.pushCalls)
	}

	updated, err := c.Update(posted.Detail.Item.ID, &commons.WantedUpdate{
		Title:       "Fix hosted auth state",
		Description: "Refresh hosted auth state after reconnect",
		Priority:    2,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Detail == nil || updated.Detail.Item == nil {
		t.Fatal("Update() should return detail with item")
	}
	if updated.Detail.Item.Title != "Fix hosted auth state" {
		t.Fatalf("updated title = %q, want Fix hosted auth state", updated.Detail.Item.Title)
	}
	if updated.Detail.Item.Description != "Refresh hosted auth state after reconnect" {
		t.Fatalf("updated description = %q, want refreshed description", updated.Detail.Item.Description)
	}
	if db.pushCalls != 2 {
		t.Fatalf("pushCalls = %d, want 2 after update", db.pushCalls)
	}
}

func TestClearBranchDataAndExtractWantedID(t *testing.T) {
	db := newFakeDB()
	db.seedItem(fakeItem{ID: "w-1", Title: "Fix bug", Status: "in_review", PostedBy: "alice", EffortLevel: "medium"})
	db.completions["w-1"] = &fakeCompletion{ID: "c-1", WantedID: "w-1", CompletedBy: "bob", Evidence: "proof"}

	branch := "wl/alice/w-1"
	db.branches[branch] = true
	db.branchItems[branch] = map[string]*fakeItem{
		"w-1": {ID: "w-1", Title: "Fix bug", Status: "in_review", PostedBy: "alice", EffortLevel: "medium"},
	}

	if err := clearBranchData(db, branch); err != nil {
		t.Fatalf("clearBranchData() error = %v", err)
	}
	if _, ok := db.branchItems[branch]["w-1"]; ok {
		t.Fatal("branch item should be deleted")
	}
	if _, ok := db.completions["w-1"]; ok {
		t.Fatal("completion should be deleted")
	}

	if got := extractWantedID(branch); got != "w-1" {
		t.Fatalf("extractWantedID(valid) = %q, want w-1", got)
	}
	if got := extractWantedID("feature/alice/w-1"); got != "" {
		t.Fatalf("extractWantedID(invalid) = %q, want empty", got)
	}
}

func TestRejectUpstream(t *testing.T) {
	var closedURL string
	c := New(ClientConfig{
		DB:        newFakeDB(),
		RigHandle: "alice",
		ListPendingItems: pendingItems(map[string][]PendingItem{
			"w-1": {{RigHandle: "charlie", Status: "in_review", PRURL: "https://dolthub.example/pulls/42"}},
		}),
		CloseUpstreamPR: func(prURL string) error {
			closedURL = prURL
			return nil
		},
	})

	if err := c.RejectUpstream("w-1", "charlie"); err != nil {
		t.Fatalf("RejectUpstream() error = %v", err)
	}
	if closedURL != "https://dolthub.example/pulls/42" {
		t.Fatalf("closed PR URL = %q, want pull URL", closedURL)
	}
}

func TestRejectUpstream_Errors(t *testing.T) {
	t.Run("missing pr url", func(t *testing.T) {
		c := New(ClientConfig{
			DB:        newFakeDB(),
			RigHandle: "alice",
			ListPendingItems: pendingItems(map[string][]PendingItem{
				"w-1": {{RigHandle: "charlie", Status: "in_review"}},
			}),
			CloseUpstreamPR: func(string) error { return nil },
		})

		err := c.RejectUpstream("w-1", "charlie")
		if err == nil || !strings.Contains(err.Error(), "submission has no upstream PR to close") {
			t.Fatalf("RejectUpstream() error = %v, want missing PR error", err)
		}
	})

	t.Run("missing submitter", func(t *testing.T) {
		c := New(ClientConfig{
			DB:               newFakeDB(),
			RigHandle:        "alice",
			ListPendingItems: pendingItems(map[string][]PendingItem{"w-1": {{RigHandle: "bob"}}}),
		})

		err := c.RejectUpstream("w-1", "charlie")
		if err == nil || !strings.Contains(err.Error(), "no pending submission from charlie") {
			t.Fatalf("RejectUpstream() error = %v, want submitter lookup error", err)
		}
	})
}

func TestCloseUpstream(t *testing.T) {
	db := newFakeDB()
	db.seedItem(fakeItem{ID: "w-1", Title: "Fix bug", Status: "in_review", PostedBy: "alice", EffortLevel: "medium"})

	var closedURL string
	c := New(ClientConfig{
		DB:        db,
		RigHandle: "alice",
		Mode:      "wild-west",
		HopURI:    "hop://alice",
		ListPendingItems: pendingItems(map[string][]PendingItem{
			"w-1": {{
				RigHandle:   "charlie",
				Status:      "in_review",
				CompletedBy: "charlie",
				Evidence:    "proof",
				PRURL:       "https://dolthub.example/pulls/9",
			}},
		}),
		CloseUpstreamPR: func(prURL string) error {
			closedURL = prURL
			return nil
		},
	})

	result, err := c.CloseUpstream("w-1", "charlie")
	if err != nil {
		t.Fatalf("CloseUpstream() error = %v", err)
	}
	if result.Detail == nil || result.Detail.Item == nil {
		t.Fatal("CloseUpstream() should return detail with item")
	}
	if result.Detail.Item.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Detail.Item.Status)
	}
	if closedURL != "https://dolthub.example/pulls/9" {
		t.Fatalf("closed PR URL = %q, want pull URL", closedURL)
	}
	if db.pushCalls != 1 {
		t.Fatalf("pushCalls = %d, want 1", db.pushCalls)
	}
}

func TestFindUpstreamSubmissionErrorsAndLeaderboard(t *testing.T) {
	t.Run("find upstream submission errors", func(t *testing.T) {
		c := New(ClientConfig{DB: newFakeDB(), RigHandle: "alice"})
		if _, err := c.findUpstreamSubmission("w-1", "charlie"); err == nil || !strings.Contains(err.Error(), "upstream PR listing not available") {
			t.Fatalf("findUpstreamSubmission(nil) error = %v", err)
		}

		c = New(ClientConfig{
			DB:        newFakeDB(),
			RigHandle: "alice",
			ListPendingItems: func() (map[string][]PendingItem, error) {
				return nil, fmt.Errorf("pending unavailable")
			},
		})
		if _, err := c.findUpstreamSubmission("w-1", "charlie"); err == nil || !strings.Contains(err.Error(), "pending unavailable") {
			t.Fatalf("findUpstreamSubmission(error) error = %v", err)
		}
	})

	t.Run("leaderboard parses aggregate rows", func(t *testing.T) {
		db := &queryOnlyDB{
			queryFunc: func(sql, _ string) (string, error) {
				switch {
				case strings.Contains(sql, "GROUP BY c.completed_by"):
					return "completed_by,completions,avg_quality,avg_reliability,avg_creativity\nalice,2,4.5,3.5,1.5\n", nil
				case strings.Contains(sql, "skill_tags"):
					return "completed_by,skill_tags\nalice,\"[\"\"go\"\",\"\"Go\"\",\"\"sql\"\"]\"\n", nil
				default:
					return "", fmt.Errorf("unexpected query: %s", sql)
				}
			},
		}

		c := New(ClientConfig{DB: db, RigHandle: "alice"})
		board, err := c.Leaderboard(0)
		if err != nil {
			t.Fatalf("Leaderboard() error = %v", err)
		}
		if len(board) != 1 {
			t.Fatalf("Leaderboard() entries = %d, want 1", len(board))
		}
		if board[0].RigHandle != "alice" || board[0].Completions != 2 {
			t.Fatalf("Leaderboard() entry = %+v", board[0])
		}
		if got := strings.Join(board[0].TopSkills, ","); got != "go,sql" {
			t.Fatalf("TopSkills = %q, want go,sql", got)
		}
	})
}
