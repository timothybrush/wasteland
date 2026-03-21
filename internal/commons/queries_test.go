package commons

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type queryTestDB struct {
	items             map[string]*WantedItem
	branchItems       map[string]map[string]*WantedItem
	completions       map[string]*CompletionRecord
	branchCompletions map[string]map[string]*CompletionRecord
	stamps            map[string]*Stamp
	branchStamps      map[string]map[string]*Stamp
	branchList        []string
	branchListErr     error
	queryLog          []string
}

func newQueryTestDB() *queryTestDB {
	return &queryTestDB{
		items:             make(map[string]*WantedItem),
		branchItems:       make(map[string]map[string]*WantedItem),
		completions:       make(map[string]*CompletionRecord),
		branchCompletions: make(map[string]map[string]*CompletionRecord),
		stamps:            make(map[string]*Stamp),
		branchStamps:      make(map[string]map[string]*Stamp),
	}
}

func (db *queryTestDB) Query(sql, ref string) (string, error) {
	db.queryLog = append(db.queryLog, sql)
	switch {
	case strings.Contains(sql, "LEFT JOIN completions") && strings.Contains(sql, "FROM wanted w"):
		return db.queryFullDetailJoined(sql, ref), nil
	case strings.Contains(sql, "FROM wanted") && strings.Contains(sql, "WHERE id"):
		return db.queryWantedByID(sql, ref), nil
	case strings.Contains(sql, "FROM wanted"):
		return db.queryWantedBrowse(sql, ref), nil
	case strings.Contains(sql, "FROM completions"):
		return db.queryCompletion(sql, ref), nil
	case strings.Contains(sql, "FROM stamps"):
		return db.queryStamp(sql, ref), nil
	default:
		return "", nil
	}
}

func (db *queryTestDB) Exec(string, string, bool, ...string) error { return nil }

func (db *queryTestDB) Branches(prefix string) ([]string, error) {
	if db.branchListErr != nil {
		return nil, db.branchListErr
	}
	if len(db.branchList) == 0 {
		return nil, nil
	}
	var branches []string
	for _, branch := range db.branchList {
		if strings.HasPrefix(branch, prefix) {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

func (db *queryTestDB) DeleteBranch(string) error          { return nil }
func (db *queryTestDB) PushBranch(string, io.Writer) error { return nil }
func (db *queryTestDB) PushMain(io.Writer) error           { return nil }
func (db *queryTestDB) Sync() error                        { return nil }
func (db *queryTestDB) MergeBranch(string) error           { return nil }
func (db *queryTestDB) DeleteRemoteBranch(string) error    { return nil }
func (db *queryTestDB) PushWithSync(io.Writer) error       { return nil }
func (db *queryTestDB) CanWildWest() error                 { return nil }

func (db *queryTestDB) queryWantedByID(sql, ref string) string {
	item := db.resolveItem(extractEqValue(sql, "id"), ref)
	if item == nil {
		switch {
		case strings.Contains(sql, "description"):
			return "id,title,description,project,type,priority,tags,posted_by,claimed_by,status,effort_level,created_at,updated_at\n"
		case strings.Contains(sql, "claimed_by"):
			return "status,claimed_by\n"
		default:
			return "status\n"
		}
	}

	switch {
	case strings.Contains(sql, "SELECT status FROM"):
		return "status\n" + item.Status + "\n"
	case strings.Contains(sql, "SELECT status,"):
		return "status,claimed_by\n" + item.Status + "," + csvCell(item.ClaimedBy) + "\n"
	default:
		return "id,title,description,project,type,priority,tags,posted_by,claimed_by,status,effort_level,created_at,updated_at\n" +
			strings.Join([]string{
				item.ID,
				csvCell(item.Title),
				csvCell(item.Description),
				csvCell(item.Project),
				csvCell(item.Type),
				itoa(item.Priority),
				csvCell(tagsJSON(item.Tags)),
				csvCell(item.PostedBy),
				csvCell(item.ClaimedBy),
				item.Status,
				csvCell(item.EffortLevel),
				csvCell(item.CreatedAt),
				csvCell(item.UpdatedAt),
			}, ",") + "\n"
	}
}

func (db *queryTestDB) queryWantedBrowse(sql, ref string) string {
	items := db.resolveItems(ref)
	long := strings.Contains(sql, "description")
	header := "id,title,project,type,priority,posted_by,claimed_by,status,effort_level"
	if long {
		header = "id,title,description,project,type,priority,posted_by,claimed_by,status,effort_level"
	}

	var rows []string
	for _, item := range items {
		if !matchesBrowseSQL(item, sql) {
			continue
		}
		row := []string{
			item.ID,
			csvCell(item.Title),
		}
		if long {
			row = append(row, csvCell(item.Description))
		}
		row = append(row,
			csvCell(item.Project),
			csvCell(item.Type),
			itoa(item.Priority),
			csvCell(item.PostedBy),
			csvCell(item.ClaimedBy),
			item.Status,
			csvCell(item.EffortLevel),
		)
		rows = append(rows, strings.Join(row, ","))
	}
	if len(rows) == 0 {
		return header + "\n"
	}
	return header + "\n" + strings.Join(rows, "\n") + "\n"
}

func (db *queryTestDB) queryCompletion(sql, ref string) string {
	record := db.resolveCompletion(extractEqValue(sql, "wanted_id"), ref)
	if record == nil {
		return "id,wanted_id,completed_by,evidence,stamp_id,validated_by\n"
	}
	return "id,wanted_id,completed_by,evidence,stamp_id,validated_by\n" +
		strings.Join([]string{
			record.ID,
			record.WantedID,
			csvCell(record.CompletedBy),
			csvCell(record.Evidence),
			csvCell(record.StampID),
			csvCell(record.ValidatedBy),
		}, ",") + "\n"
}

func (db *queryTestDB) queryFullDetailJoined(sql, ref string) string {
	item := db.resolveItem(extractEqValue(sql, "w.id"), ref)
	header := "id,title,description,project,type,priority,tags,posted_by,claimed_by,status,effort_level,created_at,updated_at,completion_id,completion_wanted_id,completed_by,evidence,completion_stamp_id,validated_by,stamp_record_id,stamp_author,stamp_subject,stamp_valence,stamp_severity,stamp_context_id,stamp_context_type,stamp_skill_tags,stamp_message\n"
	if item == nil {
		return header
	}

	completion := db.resolveCompletion(item.ID, ref)
	var stamp *Stamp
	if completion != nil && completion.StampID != "" {
		stamp = db.resolveStamp(completion.StampID, ref)
	}

	row := []string{
		item.ID,
		csvCell(item.Title),
		csvCell(item.Description),
		csvCell(item.Project),
		csvCell(item.Type),
		itoa(item.Priority),
		csvCell(tagsJSON(item.Tags)),
		csvCell(item.PostedBy),
		csvCell(item.ClaimedBy),
		item.Status,
		csvCell(item.EffortLevel),
		csvCell(item.CreatedAt),
		csvCell(item.UpdatedAt),
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
	}
	if completion != nil {
		row[13] = completion.ID
		row[14] = completion.WantedID
		row[15] = csvCell(completion.CompletedBy)
		row[16] = csvCell(completion.Evidence)
		row[17] = csvCell(completion.StampID)
		row[18] = csvCell(completion.ValidatedBy)
	}
	if stamp != nil {
		row[19] = stamp.ID
		row[20] = csvCell(stamp.Author)
		row[21] = csvCell(stamp.Subject)
		row[22] = csvCell(stampValenceJSON(stamp))
		row[23] = csvCell(stamp.Severity)
		row[24] = csvCell(stamp.ContextID)
		row[25] = csvCell(stamp.ContextType)
		row[26] = csvCell(tagsJSON(stamp.SkillTags))
		row[27] = csvCell(stamp.Message)
	}

	return header + strings.Join(row, ",") + "\n"
}

func (db *queryTestDB) queryStamp(sql, ref string) string {
	stamp := db.resolveStamp(extractEqValue(sql, "id"), ref)
	if stamp == nil {
		return "id,author,subject,valence,severity,context_id,context_type,skill_tags,message\n"
	}
	return "id,author,subject,valence,severity,context_id,context_type,skill_tags,message\n" +
		strings.Join([]string{
			stamp.ID,
			csvCell(stamp.Author),
			csvCell(stamp.Subject),
			csvCell(stampValenceJSON(stamp)),
			csvCell(stamp.Severity),
			csvCell(stamp.ContextID),
			csvCell(stamp.ContextType),
			csvCell(tagsJSON(stamp.SkillTags)),
			csvCell(stamp.Message),
		}, ",") + "\n"
}

func (db *queryTestDB) resolveItem(id, ref string) *WantedItem {
	source := db.items
	if ref != "" {
		source = db.branchItems[ref]
	}
	if source == nil || source[id] == nil {
		return nil
	}
	item := *source[id]
	return &item
}

func (db *queryTestDB) resolveItems(ref string) []*WantedItem {
	source := db.items
	if ref != "" {
		source = db.branchItems[ref]
	}
	if len(source) == 0 {
		return nil
	}
	items := make([]*WantedItem, 0, len(source))
	for _, item := range source {
		cp := *item
		items = append(items, &cp)
	}
	return items
}

func (db *queryTestDB) resolveCompletion(wantedID, ref string) *CompletionRecord {
	source := db.completions
	if ref != "" {
		source = db.branchCompletions[ref]
	}
	if source == nil || source[wantedID] == nil {
		return nil
	}
	record := *source[wantedID]
	return &record
}

func (db *queryTestDB) resolveStamp(stampID, ref string) *Stamp {
	source := db.stamps
	if ref != "" {
		source = db.branchStamps[ref]
	}
	if source == nil || source[stampID] == nil {
		return nil
	}
	stamp := *source[stampID]
	return &stamp
}

func matchesBrowseSQL(item *WantedItem, sql string) bool {
	if want := extractEqValue(sql, "status"); want != "" && item.Status != want {
		return false
	}
	if want := extractEqValue(sql, "project"); want != "" && item.Project != want {
		return false
	}
	if want := extractEqValue(sql, "type"); want != "" && item.Type != want {
		return false
	}
	if want := extractEqValue(sql, "posted_by"); want != "" && item.PostedBy != want {
		return false
	}
	if want := extractEqValue(sql, "claimed_by"); want != "" && item.ClaimedBy != want {
		return false
	}
	if want := extractNumericEqValue(sql, "priority"); want != "" && itoa(item.Priority) != want {
		return false
	}
	if want := extractLikeValue(sql, "title"); want != "" && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(want)) {
		return false
	}
	return true
}

func extractEqValue(sql, field string) string {
	for _, needle := range []string{
		field + " = '",
		field + "='",
		strings.ToLower(field) + " = '",
		strings.ToLower(field) + "='",
	} {
		if idx := strings.Index(sql, needle); idx >= 0 {
			rest := sql[idx+len(needle):]
			if end := strings.Index(rest, "'"); end >= 0 {
				return strings.ReplaceAll(rest[:end], "''", "'")
			}
		}
	}
	return ""
}

func extractNumericEqValue(sql, field string) string {
	for _, needle := range []string{
		field + " = ",
		strings.ToLower(field) + " = ",
	} {
		if idx := strings.Index(sql, needle); idx >= 0 {
			rest := sql[idx+len(needle):]
			var digits strings.Builder
			for _, ch := range rest {
				if ch < '0' || ch > '9' {
					break
				}
				digits.WriteRune(ch)
			}
			return digits.String()
		}
	}
	return ""
}

func extractLikeValue(sql, field string) string {
	for _, needle := range []string{
		field + " LIKE '%",
		strings.ToLower(field) + " LIKE '%",
	} {
		if idx := strings.Index(sql, needle); idx >= 0 {
			rest := sql[idx+len(needle):]
			if end := strings.Index(rest, "%'"); end >= 0 {
				return strings.ReplaceAll(strings.ReplaceAll(rest[:end], "\\%", "%"), "\\_", "_")
			}
		}
	}
	return ""
}

func csvCell(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func tagsJSON(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	data, _ := json.Marshal(tags)
	return string(data)
}

func stampValenceJSON(stamp *Stamp) string {
	data, _ := json.Marshal(struct {
		Quality     int `json:"quality"`
		Reliability int `json:"reliability"`
	}{
		Quality:     stamp.Quality,
		Reliability: stamp.Reliability,
	})
	return string(data)
}

func itoa(v int) string {
	data, _ := json.Marshal(v)
	return strings.Trim(string(data), `"`)
}

func TestEscapeLIKE_EscapesWildcardsAndQuotes(t *testing.T) {
	t.Parallel()

	got := EscapeLIKE(`50%_complete\it's`)
	want := `50\%\_complete\\it''s`
	if got != want {
		t.Fatalf("EscapeLIKE() = %q, want %q", got, want)
	}
}

func TestFindBranchForItem_FallsBackToBranchQueryWhenBranchesLag(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	branch := BranchName("alice", "w-1")
	db.branchItems[branch] = map[string]*WantedItem{
		"w-1": {ID: "w-1", Title: "Lagged branch item", Status: "claimed", ClaimedBy: "alice", EffortLevel: "small"},
	}

	got := FindBranchForItem(db, "alice", "w-1")
	if got != branch {
		t.Fatalf("FindBranchForItem() = %q, want %q", got, branch)
	}
}

func TestBrowseWantedBranchAware_DefaultViewTreatsEmptyAsMine(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	aliceBranch := BranchName("alice", "w-alice")
	bobBranch := BranchName("bob", "w-bob")
	db.branchList = []string{aliceBranch, bobBranch}
	db.branchItems[aliceBranch] = map[string]*WantedItem{
		"w-alice": {
			ID:          "w-alice",
			Title:       "My pending task",
			Project:     "gascity",
			Type:        "docs",
			Priority:    1,
			PostedBy:    "alice",
			Status:      "open",
			EffortLevel: "small",
		},
	}
	db.branchItems[bobBranch] = map[string]*WantedItem{
		"w-bob": {
			ID:          "w-bob",
			Title:       "Someone else's task",
			Project:     "gascity",
			Type:        "docs",
			Priority:    2,
			PostedBy:    "bob",
			Status:      "open",
			EffortLevel: "small",
		},
	}

	items, pending, err := BrowseWantedBranchAware(db, "pr", "alice", BrowseFilter{Priority: -1})
	if err != nil {
		t.Fatalf("BrowseWantedBranchAware() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "w-alice" {
		t.Fatalf("default view items = %+v, want only alice branch item", items)
	}
	if pending["w-alice"] != 1 {
		t.Fatalf("pending[w-alice] = %d, want 1", pending["w-alice"])
	}
	if pending["w-bob"] != 0 {
		t.Fatalf("pending[w-bob] = %d, want 0", pending["w-bob"])
	}
}

func TestBrowseWantedBranchAware_IncludesBranchOnlyClaimedItemsInClaimedByView(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	db.items["w-1"] = &WantedItem{
		ID:          "w-1",
		Title:       "Take branch claimant from override",
		Project:     "gascity",
		Type:        "bug",
		Priority:    1,
		PostedBy:    "alice",
		Status:      "open",
		EffortLevel: "medium",
	}
	branch := BranchName("alice", "w-1")
	db.branchList = []string{branch}
	db.branchItems[branch] = map[string]*WantedItem{
		"w-1": {
			ID:          "w-1",
			Title:       "Take branch claimant from override",
			Project:     "gascity",
			Type:        "bug",
			Priority:    1,
			PostedBy:    "alice",
			ClaimedBy:   "alice",
			Status:      "claimed",
			EffortLevel: "medium",
		},
	}

	items, pending, err := BrowseWantedBranchAware(db, "pr", "alice", BrowseFilter{
		Status:    "claimed",
		ClaimedBy: "alice",
		Priority:  -1,
	})
	if err != nil {
		t.Fatalf("BrowseWantedBranchAware() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1 (%+v)", len(items), items)
	}
	if items[0].ID != "w-1" {
		t.Fatalf("items[0].ID = %q, want w-1", items[0].ID)
	}
	if items[0].ClaimedBy != "alice" {
		t.Fatalf("items[0].ClaimedBy = %q, want alice", items[0].ClaimedBy)
	}
	if items[0].Status != "claimed" {
		t.Fatalf("items[0].Status = %q, want claimed", items[0].Status)
	}
	if pending["w-1"] != 1 {
		t.Fatalf("pending[w-1] = %d, want 1", pending["w-1"])
	}
}

func TestQueryMyDashboardBranchAware_UsesBranchClaimedByForBranchOnlyClaimedItems(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	db.items["w-1"] = &WantedItem{
		ID:          "w-1",
		Title:       "Dashboard branch overlay",
		Project:     "gascity",
		Type:        "feature",
		Priority:    2,
		PostedBy:    "bob",
		Status:      "open",
		EffortLevel: "small",
	}
	branch := BranchName("alice", "w-1")
	db.branchList = []string{branch}
	db.branchItems[branch] = map[string]*WantedItem{
		"w-1": {
			ID:          "w-1",
			Title:       "Dashboard branch overlay",
			Project:     "gascity",
			Type:        "feature",
			Priority:    2,
			PostedBy:    "bob",
			ClaimedBy:   "alice",
			Status:      "claimed",
			EffortLevel: "small",
		},
	}

	data, err := QueryMyDashboardBranchAware(db, "pr", "alice")
	if err != nil {
		t.Fatalf("QueryMyDashboardBranchAware() error = %v", err)
	}
	if len(data.Claimed) != 1 {
		t.Fatalf("len(data.Claimed) = %d, want 1 (%+v)", len(data.Claimed), data.Claimed)
	}
	if data.Claimed[0].ID != "w-1" {
		t.Fatalf("data.Claimed[0].ID = %q, want w-1", data.Claimed[0].ID)
	}
	if data.Claimed[0].ClaimedBy != "alice" {
		t.Fatalf("data.Claimed[0].ClaimedBy = %q, want alice", data.Claimed[0].ClaimedBy)
	}
	if data.Claimed[0].Status != "claimed" {
		t.Fatalf("data.Claimed[0].Status = %q, want claimed", data.Claimed[0].Status)
	}
}

func TestResolveItemState_UsesBranchCompletionAndStamp(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	db.items["w-1"] = &WantedItem{
		ID:          "w-1",
		Title:       "Review me",
		Status:      "open",
		PostedBy:    "alice",
		EffortLevel: "medium",
	}
	branch := BranchName("alice", "w-1")
	db.branchList = []string{branch}
	db.branchItems[branch] = map[string]*WantedItem{
		"w-1": {
			ID:          "w-1",
			Title:       "Review me",
			Status:      "completed",
			PostedBy:    "alice",
			ClaimedBy:   "alice",
			EffortLevel: "medium",
		},
	}
	db.branchCompletions[branch] = map[string]*CompletionRecord{
		"w-1": {
			ID:          "c-1",
			WantedID:    "w-1",
			CompletedBy: "alice",
			Evidence:    "done",
			StampID:     "s-1",
			ValidatedBy: "bob",
		},
	}
	db.branchStamps[branch] = map[string]*Stamp{
		"s-1": {
			ID:          "s-1",
			Author:      "bob",
			Subject:     "alice",
			Quality:     4,
			Reliability: 5,
			Severity:    "medium",
			ContextID:   "w-1",
			ContextType: "wanted",
			SkillTags:   []string{"go", "testing"},
			Message:     "solid fix",
		},
	}

	state, err := ResolveItemState(db, "alice", "w-1")
	if err != nil {
		t.Fatalf("ResolveItemState() error = %v", err)
	}
	if state.BranchName != branch {
		t.Fatalf("BranchName = %q, want %q", state.BranchName, branch)
	}
	if state.EffectiveStatus() != "completed" {
		t.Fatalf("EffectiveStatus() = %q, want completed", state.EffectiveStatus())
	}
	if state.Delta() == "" {
		t.Fatal("Delta() should describe the branch change")
	}
	if state.Completion == nil || state.Completion.ID != "c-1" {
		t.Fatalf("Completion = %+v, want branch completion", state.Completion)
	}
	if state.Stamp == nil || state.Stamp.ID != "s-1" {
		t.Fatalf("Stamp = %+v, want branch stamp", state.Stamp)
	}
	if state.Stamp.Quality != 4 || state.Stamp.Reliability != 5 {
		t.Fatalf("Stamp valence = %+v, want quality=4 reliability=5", state.Stamp)
	}
}

func TestResolveItemState_UsesJoinedQueriesWithoutSeparateCompletionOrStampReads(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	db.items["w-1"] = &WantedItem{
		ID:          "w-1",
		Title:       "Branch detail",
		Status:      "open",
		PostedBy:    "alice",
		EffortLevel: "small",
	}
	branch := BranchName("alice", "w-1")
	db.branchList = []string{branch}
	db.branchItems[branch] = map[string]*WantedItem{
		"w-1": {
			ID:          "w-1",
			Title:       "Branch detail",
			Status:      "in_review",
			PostedBy:    "alice",
			ClaimedBy:   "alice",
			EffortLevel: "small",
		},
	}
	db.branchCompletions[branch] = map[string]*CompletionRecord{
		"w-1": {
			ID:          "c-1",
			WantedID:    "w-1",
			CompletedBy: "alice",
			Evidence:    "https://example.com/proof",
			StampID:     "s-1",
		},
	}
	db.branchStamps[branch] = map[string]*Stamp{
		"s-1": {
			ID:          "s-1",
			Author:      "bob",
			Subject:     "alice",
			Quality:     4,
			Reliability: 5,
			Severity:    "medium",
		},
	}

	state, err := ResolveItemState(db, "alice", "w-1")
	if err != nil {
		t.Fatalf("ResolveItemState() error = %v", err)
	}
	if state.Branch == nil || state.Completion == nil || state.Stamp == nil {
		t.Fatalf("state = %+v, want branch detail with completion and stamp", state)
	}

	var joinedQueries, completionQueries, stampQueries int
	for _, sql := range db.queryLog {
		switch {
		case strings.Contains(sql, "LEFT JOIN completions"):
			joinedQueries++
		case strings.Contains(sql, "FROM completions"):
			completionQueries++
		case strings.Contains(sql, "FROM stamps"):
			stampQueries++
		}
	}
	if joinedQueries != 2 {
		t.Fatalf("joinedQueries = %d, want 2 (main + branch)", joinedQueries)
	}
	if completionQueries != 0 {
		t.Fatalf("completionQueries = %d, want 0", completionQueries)
	}
	if stampQueries != 0 {
		t.Fatalf("stampQueries = %d, want 0", stampQueries)
	}
}

func TestValidBrowseCyclesAndLabels(t *testing.T) {
	t.Parallel()

	if got := ValidSortOrders(); len(got) != 3 || got[0] != SortPriority || got[1] != SortNewest || got[2] != SortAlpha {
		t.Fatalf("ValidSortOrders() = %+v", got)
	}
	if got := ValidStatuses(); len(got) != 5 || got[0] != "open" || got[4] != "" {
		t.Fatalf("ValidStatuses() = %+v", got)
	}
	if got := ValidTypes(); len(got) != 7 || got[0] != "" || got[6] != "inference" {
		t.Fatalf("ValidTypes() = %+v", got)
	}
	if got := StatusLabel(""); got != "all" {
		t.Fatalf("StatusLabel(\"\") = %q, want all", got)
	}
	if got := TypeLabel(""); got != "all" {
		t.Fatalf("TypeLabel(\"\") = %q, want all", got)
	}
	if got := SortLabel(99); got != "priority" {
		t.Fatalf("SortLabel(99) = %q, want priority", got)
	}
}

func TestDetectAllBranchOverrides_CountsTouchesAndDeduplicates(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	db.items["w-1"] = &WantedItem{ID: "w-1", Title: "Base", Status: "open", EffortLevel: "small"}
	db.branchList = []string{
		BranchName("alice", "w-1"),
		BranchName("bob", "w-1"),
		"wl/charlie/",
	}
	db.branchItems[BranchName("alice", "w-1")] = map[string]*WantedItem{
		"w-1": {ID: "w-1", Title: "Base", Status: "claimed", ClaimedBy: "alice", EffortLevel: "small"},
	}
	db.branchItems[BranchName("bob", "w-1")] = map[string]*WantedItem{
		"w-1": {ID: "w-1", Title: "Base", Status: "in_review", ClaimedBy: "bob", EffortLevel: "small"},
	}

	overrides, counts := DetectAllBranchOverrides(db)
	if counts["w-1"] != 2 {
		t.Fatalf("counts[w-1] = %d, want 2", counts["w-1"])
	}
	if len(overrides) != 1 {
		t.Fatalf("len(overrides) = %d, want 1 (%+v)", len(overrides), overrides)
	}
	if overrides[0].WantedID != "w-1" || overrides[0].Branch != BranchName("alice", "w-1") {
		t.Fatalf("override = %+v", overrides[0])
	}
}

func TestQueryMyDashboardBranchAware_AppliesBranchOverrides(t *testing.T) {
	t.Parallel()

	db := newQueryTestDB()
	db.items["w-claim"] = &WantedItem{
		ID:          "w-claim",
		Title:       "Claimed task",
		Status:      "claimed",
		PostedBy:    "bob",
		ClaimedBy:   "alice",
		EffortLevel: "small",
	}
	db.items["w-review"] = &WantedItem{
		ID:          "w-review",
		Title:       "Review task",
		Status:      "in_review",
		PostedBy:    "alice",
		ClaimedBy:   "alice",
		EffortLevel: "small",
	}
	db.branchList = []string{BranchName("alice", "w-claim")}
	db.branchItems[BranchName("alice", "w-claim")] = map[string]*WantedItem{
		"w-claim": {
			ID:          "w-claim",
			Title:       "Claimed task",
			Status:      "completed",
			PostedBy:    "bob",
			ClaimedBy:   "alice",
			EffortLevel: "small",
		},
	}

	data, err := QueryMyDashboardBranchAware(db, "pr", "alice")
	if err != nil {
		t.Fatalf("QueryMyDashboardBranchAware() error = %v", err)
	}
	if len(data.Claimed) != 0 {
		t.Fatalf("Claimed = %+v, want empty after override", data.Claimed)
	}
	if len(data.Completed) != 1 || data.Completed[0].ID != "w-claim" {
		t.Fatalf("Completed = %+v, want w-claim", data.Completed)
	}
	if len(data.InReview) != 1 || data.InReview[0].ID != "w-review" {
		t.Fatalf("InReview = %+v, want w-review", data.InReview)
	}
}

func TestParseWantedSummaries_DefaultsMalformedPriority(t *testing.T) {
	t.Parallel()

	items := parseWantedSummaries("id,title,priority,status,effort_level\nw-1,Task,not-a-number,open,small\n")
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Priority != 2 {
		t.Fatalf("Priority = %d, want default 2", items[0].Priority)
	}
}

var _ DB = (*queryTestDB)(nil)
