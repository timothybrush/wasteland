package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/pile"
	"github.com/gastownhall/wasteland/internal/remote"
	"github.com/gastownhall/wasteland/internal/sdk"
	"github.com/spf13/cobra"
)

type scriptedDB struct {
	noopDB
	queryFunc        func(string, string) (string, error)
	branchesFunc     func(string) ([]string, error)
	syncFunc         func() error
	mergeBranchFunc  func(string) error
	deleteBranchFunc func(string) error
}

func (db scriptedDB) Query(sql, ref string) (string, error) {
	if db.queryFunc != nil {
		return db.queryFunc(sql, ref)
	}
	return db.noopDB.Query(sql, ref)
}

func (db scriptedDB) Branches(prefix string) ([]string, error) {
	if db.branchesFunc != nil {
		return db.branchesFunc(prefix)
	}
	return db.noopDB.Branches(prefix)
}

func (db scriptedDB) Sync() error {
	if db.syncFunc != nil {
		return db.syncFunc()
	}
	return db.noopDB.Sync()
}

func (db scriptedDB) MergeBranch(branch string) error {
	if db.mergeBranchFunc != nil {
		return db.mergeBranchFunc(branch)
	}
	return db.noopDB.MergeBranch(branch)
}

func (db scriptedDB) DeleteBranch(branch string) error {
	if db.deleteBranchFunc != nil {
		return db.deleteBranchFunc(branch)
	}
	return db.noopDB.DeleteBranch(branch)
}

func commandWithWasteland(upstream string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("wasteland", "", "")
	cmd.Flags().Bool("local-db", false, "")
	if upstream != "" {
		_ = cmd.Flags().Set("wasteland", upstream)
	}
	return cmd
}

type fakeCommandClient struct {
	detailFn  func(string) (*sdk.DetailResult, error)
	claimFn   func(string) (*sdk.MutationResult, error)
	unclaimFn func(string) (*sdk.MutationResult, error)
	doneFn    func(string, string) (*sdk.MutationResult, error)
	acceptFn  func(string, sdk.AcceptInput) (*sdk.MutationResult, error)
	rejectFn  func(string, string) (*sdk.MutationResult, error)
	closeFn   func(string) (*sdk.MutationResult, error)
	deleteFn  func(string) (*sdk.MutationResult, error)
	postFn    func(sdk.PostInput) (*sdk.MutationResult, error)
	updateFn  func(string, *commons.WantedUpdate) (*sdk.MutationResult, error)
}

func (f fakeCommandClient) Detail(wantedID string) (*sdk.DetailResult, error) {
	if f.detailFn != nil {
		return f.detailFn(wantedID)
	}
	return nil, nil
}

func (f fakeCommandClient) Claim(wantedID string) (*sdk.MutationResult, error) {
	if f.claimFn != nil {
		return f.claimFn(wantedID)
	}
	return nil, nil
}

func (f fakeCommandClient) Unclaim(wantedID string) (*sdk.MutationResult, error) {
	if f.unclaimFn != nil {
		return f.unclaimFn(wantedID)
	}
	return nil, nil
}

func (f fakeCommandClient) Done(wantedID, evidence string) (*sdk.MutationResult, error) {
	if f.doneFn != nil {
		return f.doneFn(wantedID, evidence)
	}
	return nil, nil
}

func (f fakeCommandClient) Accept(wantedID string, input sdk.AcceptInput) (*sdk.MutationResult, error) {
	if f.acceptFn != nil {
		return f.acceptFn(wantedID, input)
	}
	return nil, nil
}

func (f fakeCommandClient) Reject(wantedID, reason string) (*sdk.MutationResult, error) {
	if f.rejectFn != nil {
		return f.rejectFn(wantedID, reason)
	}
	return nil, nil
}

func (f fakeCommandClient) Close(wantedID string) (*sdk.MutationResult, error) {
	if f.closeFn != nil {
		return f.closeFn(wantedID)
	}
	return nil, nil
}

func (f fakeCommandClient) Delete(wantedID string) (*sdk.MutationResult, error) {
	if f.deleteFn != nil {
		return f.deleteFn(wantedID)
	}
	return nil, nil
}

func (f fakeCommandClient) Post(input sdk.PostInput) (*sdk.MutationResult, error) {
	if f.postFn != nil {
		return f.postFn(input)
	}
	return nil, nil
}

func (f fakeCommandClient) Update(wantedID string, fields *commons.WantedUpdate) (*sdk.MutationResult, error) {
	if f.updateFn != nil {
		return f.updateFn(wantedID, fields)
	}
	return nil, nil
}

func withOpenDBFromConfigOverride(t *testing.T, fn func(*federation.Config) (commons.DB, error)) {
	t.Helper()
	old := openDBFromConfig
	openDBFromConfig = fn
	t.Cleanup(func() {
		openDBFromConfig = old
	})
}

func withOpenDBOverride(t *testing.T, fn func(string) commons.DB) {
	t.Helper()
	old := openDB
	openDB = fn
	t.Cleanup(func() {
		openDB = old
	})
}

func withCommandClientOverride(t *testing.T, fn func(*federation.Config, bool) (commandClient, error)) {
	t.Helper()
	old := newCommandClient
	newCommandClient = fn
	t.Cleanup(func() {
		newCommandClient = old
	})
}

func withResolveWantedArgOverride(t *testing.T, fn func(*federation.Config, string) (string, error)) {
	t.Helper()
	old := resolveWantedArg
	resolveWantedArg = fn
	t.Cleanup(func() {
		resolveWantedArg = old
	})
}

func withPileOverrides(
	t *testing.T,
	newClientFn func() *pile.Client,
	queryFn func(pile.RowQuerier, string) (*pile.Profile, error),
	searchFn func(pile.RowQuerier, string, int) ([]pile.ProfileSummary, error),
) {
	t.Helper()
	oldNewClient := newPileClient
	oldQuery := queryPileProfile
	oldSearch := searchPileProfiles
	if newClientFn != nil {
		newPileClient = newClientFn
	}
	if queryFn != nil {
		queryPileProfile = queryFn
	}
	if searchFn != nil {
		searchPileProfiles = searchFn
	}
	t.Cleanup(func() {
		newPileClient = oldNewClient
		queryPileProfile = oldQuery
		searchPileProfiles = oldSearch
	})
}

func withLeaderboardOverride(t *testing.T, fn func(commons.DB, int) ([]commons.LeaderboardEntry, error)) {
	t.Helper()
	old := queryLeaderboard
	queryLeaderboard = fn
	t.Cleanup(func() {
		queryLeaderboard = old
	})
}

func withPendingWantedStatesOverride(
	t *testing.T,
	fn func(string, string, string) (map[string][]remote.PendingWantedState, error),
) {
	t.Helper()
	old := listPendingWantedStates
	listPendingWantedStates = fn
	t.Cleanup(func() {
		listPendingWantedStates = old
	})
}

func installFakeDolt(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dolt")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake dolt: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

func installFakeCommand(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}
