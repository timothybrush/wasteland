package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
	"github.com/gastownhall/wasteland/internal/tui"
	"github.com/spf13/cobra"
)

type fakeLocalWorkflowDB struct {
	scriptedDB
	pushMainFn func(io.Writer) error
}

func (db fakeLocalWorkflowDB) PushMain(w io.Writer) error {
	if db.pushMainFn != nil {
		return db.pushMainFn(w)
	}
	return nil
}

type fakeRemoteWorkflowDB struct {
	scriptedDB
	diffFn func(string) (string, error)
}

func (db fakeRemoteWorkflowDB) Diff(branch string) (string, error) {
	if db.diffFn != nil {
		return db.diffFn(branch)
	}
	return "", nil
}

type fakeTeaProgram struct {
	runFn func() error
}

func (p fakeTeaProgram) Run() (bubbletea.Model, error) {
	if p.runFn != nil {
		return nil, p.runFn()
	}
	return nil, nil
}

type fakeJoinRemoteProvider struct {
	forkFn        func(string, string, string) error
	createPRFn    func(string, string, string, string, string, string) (string, error)
	databaseURLFn func(string, string) string
}

func (p fakeJoinRemoteProvider) Fork(fromOrg, fromDB, toOrg string) error {
	if p.forkFn != nil {
		return p.forkFn(fromOrg, fromDB, toOrg)
	}
	return nil
}

func (p fakeJoinRemoteProvider) CreatePR(forkOrg, upstreamOrg, db, fromBranch, title, body string) (string, error) {
	if p.createPRFn != nil {
		return p.createPRFn(forkOrg, upstreamOrg, db, fromBranch, title, body)
	}
	return "", nil
}

func (p fakeJoinRemoteProvider) DatabaseURL(org, db string) string {
	if p.databaseURLFn != nil {
		return p.databaseURLFn(org, db)
	}
	return ""
}

type fakeJoinRemoteDB struct {
	execFn func(string, string, bool, ...string) error
}

func (db fakeJoinRemoteDB) Exec(branch, ref string, allowEmpty bool, stmts ...string) error {
	if db.execFn != nil {
		return db.execFn(branch, ref, allowEmpty, stmts...)
	}
	return nil
}

func withCreateWithProviderOverride(
	t *testing.T,
	fn func(io.Writer, remote.Provider, federation.ConfigStore, federation.CreateOptions) (*federation.CreateResult, error),
) {
	t.Helper()
	old := createWithProvider
	createWithProvider = fn
	t.Cleanup(func() {
		createWithProvider = old
	})
}

func withJoinWithProviderOverride(
	t *testing.T,
	fn func(io.Writer, remote.Provider, federation.ConfigStore, string, string, string, string, string, string, bool, bool) (*federation.JoinResult, error),
) {
	t.Helper()
	old := joinWithProvider
	joinWithProvider = fn
	t.Cleanup(func() {
		joinWithProvider = old
	})
}

func withLocalWorkflowDBOverride(t *testing.T, fn func(string, string) localWorkflowDB) {
	t.Helper()
	old := newLocalWorkflowDB
	newLocalWorkflowDB = fn
	t.Cleanup(func() {
		newLocalWorkflowDB = old
	})
}

func withRemoteWorkflowDBOverride(t *testing.T, fn func(string, string, string, string, string, string) remoteWorkflowDB) {
	t.Helper()
	old := newRemoteWorkflowDB
	newRemoteWorkflowDB = fn
	t.Cleanup(func() {
		newRemoteWorkflowDB = old
	})
}

func withHostedPublicDBOverride(t *testing.T, fn func() commons.DB) {
	t.Helper()
	old := newHostedPublicDB
	newHostedPublicDB = fn
	t.Cleanup(func() {
		newHostedPublicDB = old
	})
}

func withServeListenOverride(t *testing.T, fn func(*http.Server) error) {
	t.Helper()
	old := serveListen
	serveListen = fn
	t.Cleanup(func() {
		serveListen = old
	})
}

func withTeaOverrides(
	t *testing.T,
	modelFn func(tui.Config) bubbletea.Model,
	programFn func(bubbletea.Model, ...bubbletea.ProgramOption) teaProgram,
) {
	t.Helper()
	oldModel := newTUIModel
	oldProgram := newTeaProgram
	if modelFn != nil {
		newTUIModel = modelFn
	}
	if programFn != nil {
		newTeaProgram = programFn
	}
	t.Cleanup(func() {
		newTUIModel = oldModel
		newTeaProgram = oldProgram
	})
}

func withJoinRemoteOverrides(
	t *testing.T,
	providerFn func(string) joinRemoteProvider,
	dbFn func(string, string, string, string, string, string) joinRemoteDB,
) {
	t.Helper()
	oldProvider := newJoinRemoteProvider
	oldDB := newJoinRemoteDB
	if providerFn != nil {
		newJoinRemoteProvider = providerFn
	}
	if dbFn != nil {
		newJoinRemoteDB = dbFn
	}
	t.Cleanup(func() {
		newJoinRemoteProvider = oldProvider
		newJoinRemoteDB = oldDB
	})
}

func TestRunCreate_UsesInjectedCreateFlow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	installFakeDolt(t, "#!/bin/sh\nexit 0\n")

	var gotProviderType string
	var gotOpts federation.CreateOptions
	withCreateWithProviderOverride(t, func(_ io.Writer, provider remote.Provider, _ federation.ConfigStore, opts federation.CreateOptions) (*federation.CreateResult, error) {
		gotProviderType = provider.Type()
		gotOpts = opts
		return &federation.CreateResult{Config: &federation.Config{
			Upstream:  opts.Upstream,
			LocalDir:  "/tmp/wl-commons",
			RigHandle: opts.Handle,
		}}, nil
	})

	var stdout bytes.Buffer
	if err := runCreate(&stdout, io.Discard, "alice/wl-commons", "Alice Town", "alice", "Alice", "alice@example.com", "/tmp/remotes", "", false, "", false, false); err != nil {
		t.Fatalf("runCreate() error = %v", err)
	}
	if gotProviderType != "file" {
		t.Fatalf("provider type = %q", gotProviderType)
	}
	if gotOpts.Name != "Alice Town" || gotOpts.Handle != "alice" || gotOpts.OwnerEmail != "alice@example.com" {
		t.Fatalf("opts = %+v", gotOpts)
	}
	if out := stdout.String(); !strings.Contains(out, "Created wasteland: alice/wl-commons") || !strings.Contains(out, "Share: wl join alice/wl-commons") {
		t.Fatalf("output = %q", out)
	}
}

func TestRunJoin_UsesInjectedJoinFlowAndForkInstructions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	installFakeDolt(t, "#!/bin/sh\nexit 0\n")

	t.Run("success", func(t *testing.T) {
		var gotProviderType string
		withJoinWithProviderOverride(t, func(_ io.Writer, provider remote.Provider, _ federation.ConfigStore, upstream, forkOrg, handle, displayName, email, version string, signed, direct bool) (*federation.JoinResult, error) {
			gotProviderType = provider.Type()
			if upstream != "hop/wl-commons" || forkOrg != "alice-org" || handle != "alice" || email != "alice@example.com" || version != "dev" || signed || direct {
				t.Fatalf("got %q %q %q %q %q %q %v %v", upstream, forkOrg, handle, displayName, email, version, signed, direct)
			}
			return &federation.JoinResult{
				Config: &federation.Config{
					Upstream:  upstream,
					ForkOrg:   forkOrg,
					ForkDB:    "wl-commons",
					LocalDir:  "/tmp/wl-commons",
					RigHandle: handle,
				},
				PRURL: "https://example/pr/1",
			}, nil
		})

		var stdout bytes.Buffer
		err := runJoin(&stdout, io.Discard, "hop/wl-commons", "alice", "Alice", "alice@example.com", "alice-org", "/tmp/remotes", "", false, "", false, false)
		if err != nil {
			t.Fatalf("runJoin() error = %v", err)
		}
		if gotProviderType != "file" {
			t.Fatalf("provider type = %q", gotProviderType)
		}
		if out := stdout.String(); !strings.Contains(out, "Joined wasteland: hop/wl-commons") || !strings.Contains(out, "PR:") {
			t.Fatalf("output = %q", out)
		}
	})

	t.Run("fork required", func(t *testing.T) {
		withJoinWithProviderOverride(t, func(_ io.Writer, _ remote.Provider, _ federation.ConfigStore, _, _, _, _, _, _ string, _, _ bool) (*federation.JoinResult, error) {
			return nil, &remote.ForkRequiredError{
				UpstreamOrg: "hop",
				UpstreamDB:  "wl-commons",
				ForkOrg:     "alice",
			}
		})

		var stdout bytes.Buffer
		err := runJoin(&stdout, io.Discard, "hop/wl-commons", "alice", "Alice", "alice@example.com", "alice", "/tmp/remotes", "", false, "", false, false)
		if !errors.Is(err, errExit) {
			t.Fatalf("err = %v", err)
		}
		if out := stdout.String(); !strings.Contains(out, "Fork required") || !strings.Contains(out, "wl join") {
			t.Fatalf("output = %q", out)
		}
	})
}

func TestRunJoinRemote_UsesInjectedProviderAndDB(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DOLTHUB_TOKEN", "token")
	t.Setenv("DOLTHUB_ORG", "alice-org")

	var executed []string
	withJoinRemoteOverrides(
		t,
		func(token string) joinRemoteProvider {
			if token != "token" {
				t.Fatalf("token = %q", token)
			}
			return fakeJoinRemoteProvider{
				forkFn: func(fromOrg, fromDB, toOrg string) error {
					if fromOrg != "hop" || fromDB != "wl-commons" || toOrg != "alice-org" {
						t.Fatalf("fork got %q %q %q", fromOrg, fromDB, toOrg)
					}
					return nil
				},
				createPRFn: func(_, _, _, branch, title, body string) (string, error) {
					if branch != "wl/register/alice" || !strings.Contains(title, "alice") || !strings.Contains(body, "Alice") {
						t.Fatalf("createPR got %q %q %q", branch, title, body)
					}
					return "https://example/pr/2", nil
				},
				databaseURLFn: func(org, db string) string {
					return "https://example/" + org + "/" + db
				},
			}
		},
		func(_, _, _, _, _, _ string) joinRemoteDB {
			return fakeJoinRemoteDB{
				execFn: func(branch, ref string, allowEmpty bool, stmts ...string) error {
					executed = append(executed, branch, ref, fmt.Sprintf("%v", allowEmpty), strings.Join(stmts, "\n"))
					return nil
				},
			}
		},
	)

	var stdout bytes.Buffer
	err := runJoinRemote(&stdout, io.Discard, "hop/wl-commons", "alice", "Alice", "alice@example.com", "alice-org")
	if err != nil {
		t.Fatalf("runJoinRemote() error = %v", err)
	}
	if len(executed) == 0 || !strings.Contains(executed[3], "INSERT") {
		t.Fatalf("executed = %+v", executed)
	}
	if out := stdout.String(); !strings.Contains(out, "Joined wasteland: hop/wl-commons (remote mode)") || !strings.Contains(out, "Backend: remote (DoltHub API)") {
		t.Fatalf("output = %q", out)
	}
}

func TestRunJoinRemote_ValidationAndWarnings(t *testing.T) {
	t.Run("invalid upstream", func(t *testing.T) {
		err := runJoinRemote(io.Discard, io.Discard, "bad-upstream", "alice", "Alice", "alice@example.com", "alice-org")
		if err == nil || !strings.Contains(err.Error(), "invalid upstream path") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "")
		t.Setenv("DOLTHUB_ORG", "alice-org")
		err := runJoinRemote(io.Discard, io.Discard, "hop/wl-commons", "alice", "Alice", "alice@example.com", "")
		if err == nil || !strings.Contains(err.Error(), "DOLTHUB_TOKEN environment variable is required") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("missing org", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		t.Setenv("DOLTHUB_ORG", "")
		err := runJoinRemote(io.Discard, io.Discard, "hop/wl-commons", "alice", "Alice", "alice@example.com", "")
		if err == nil || !strings.Contains(err.Error(), "DOLTHUB_ORG environment variable is required") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("create pr warning", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		t.Setenv("DOLTHUB_ORG", "alice-org")

		withJoinRemoteOverrides(
			t,
			func(string) joinRemoteProvider {
				return fakeJoinRemoteProvider{
					forkFn: func(string, string, string) error { return nil },
					createPRFn: func(string, string, string, string, string, string) (string, error) {
						return "", fmt.Errorf("pr failed")
					},
					databaseURLFn: func(org, db string) string {
						return "https://example/" + org + "/" + db
					},
				}
			},
			func(_, _, _, _, _, _ string) joinRemoteDB {
				return fakeJoinRemoteDB{
					execFn: func(string, string, bool, ...string) error { return nil },
				}
			},
		)

		var stdout bytes.Buffer
		if err := runJoinRemote(&stdout, io.Discard, "hop/wl-commons", "alice", "Alice", "alice@example.com", "alice-org"); err != nil {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(stdout.String(), "warning: could not create PR: pr failed") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})
}

func TestRunTUI_LocalAndProgramErrorPaths(t *testing.T) {
	t.Run("local success", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
		installFakeDolt(t, "#!/bin/sh\nexit 0\n")

		var synced, pushed bool
		withLocalWorkflowDBOverride(t, func(string, string) localWorkflowDB {
			return fakeLocalWorkflowDB{
				pushMainFn: func(io.Writer) error {
					pushed = true
					return nil
				},
				scriptedDB: scriptedDB{
					syncFunc: func() error {
						synced = true
						return nil
					},
				},
			}
		})
		withTeaOverrides(t, func(tui.Config) bubbletea.Model { return nil }, func(bubbletea.Model, ...bubbletea.ProgramOption) teaProgram {
			return fakeTeaProgram{}
		})

		cmd := commandWithWasteland("hop/wl-commons")
		_ = cmd.Flags().Set("local-db", "true")
		if err := runTUI(cmd, io.Discard, io.Discard); err != nil {
			t.Fatalf("runTUI(local) error = %v", err)
		}
		if !synced || !pushed {
			t.Fatalf("synced = %v pushed = %v", synced, pushed)
		}
	})

	t.Run("local sync failure", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
		installFakeDolt(t, "#!/bin/sh\nexit 0\n")
		withLocalWorkflowDBOverride(t, func(string, string) localWorkflowDB {
			return fakeLocalWorkflowDB{
				scriptedDB: scriptedDB{
					syncFunc: func() error { return fmt.Errorf("boom") },
				},
			}
		})

		cmd := commandWithWasteland("hop/wl-commons")
		_ = cmd.Flags().Set("local-db", "true")
		err := runTUI(cmd, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "syncing with upstream: boom") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("program error", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
		installFakeDolt(t, "#!/bin/sh\nexit 0\n")
		withLocalWorkflowDBOverride(t, func(string, string) localWorkflowDB {
			return fakeLocalWorkflowDB{scriptedDB: scriptedDB{}}
		})
		withTeaOverrides(t, func(tui.Config) bubbletea.Model { return nil }, func(bubbletea.Model, ...bubbletea.ProgramOption) teaProgram {
			return fakeTeaProgram{runFn: func() error { return fmt.Errorf("boom") }}
		})

		cmd := commandWithWasteland("hop/wl-commons")
		_ = cmd.Flags().Set("local-db", "true")
		err := runTUI(cmd, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "TUI error: boom") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("remote success", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			RigHandle: "alice",
			Backend:   federation.BackendRemote,
			JoinedAt:  time.Now(),
		})

		var synced bool
		withRemoteWorkflowDBOverride(t, func(token, upstreamOrg, upstreamDB, forkOrg, forkDB, mode string) remoteWorkflowDB {
			if token != "token" || upstreamOrg != "hop" || upstreamDB != "wl-commons" || forkOrg != "alice" || forkDB != "wl-commons" || mode != federation.ModePR {
				t.Fatalf("got %q %q %q %q %q %q", token, upstreamOrg, upstreamDB, forkOrg, forkDB, mode)
			}
			return fakeRemoteWorkflowDB{
				scriptedDB: scriptedDB{
					syncFunc: func() error {
						synced = true
						return nil
					},
				},
				diffFn: func(string) (string, error) { return "diff", nil },
			}
		})
		withTeaOverrides(t, func(tui.Config) bubbletea.Model { return nil }, func(bubbletea.Model, ...bubbletea.ProgramOption) teaProgram {
			return fakeTeaProgram{}
		})

		if err := runTUI(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard); err != nil {
			t.Fatalf("runTUI(remote) error = %v", err)
		}
		if !synced {
			t.Fatal("remote sync was not called")
		}
	})

	t.Run("remote sync warning", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			RigHandle: "alice",
			Backend:   federation.BackendRemote,
			JoinedAt:  time.Now(),
		})
		withRemoteWorkflowDBOverride(t, func(string, string, string, string, string, string) remoteWorkflowDB {
			return fakeRemoteWorkflowDB{
				scriptedDB: scriptedDB{
					syncFunc: func() error { return fmt.Errorf("out of date") },
				},
			}
		})
		withTeaOverrides(t, func(tui.Config) bubbletea.Model { return nil }, func(bubbletea.Model, ...bubbletea.ProgramOption) teaProgram {
			return fakeTeaProgram{}
		})

		var stderr bytes.Buffer
		if err := runTUI(commandWithWasteland("hop/wl-commons"), io.Discard, &stderr); err != nil {
			t.Fatalf("runTUI(remote warning) error = %v", err)
		}
		if !strings.Contains(stderr.String(), "fork sync skipped") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})
}

func TestRunServe_LocalRemoteAndHostedPaths(t *testing.T) {
	t.Run("local success", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
		installFakeDolt(t, "#!/bin/sh\nexit 0\n")

		var synced, pushed bool
		withLocalWorkflowDBOverride(t, func(string, string) localWorkflowDB {
			return fakeLocalWorkflowDB{
				pushMainFn: func(io.Writer) error {
					pushed = true
					return nil
				},
				scriptedDB: scriptedDB{
					syncFunc: func() error {
						synced = true
						return nil
					},
					queryFunc: func(string, string) (string, error) { return "", nil },
				},
			}
		})
		withServeListenOverride(t, func(srv *http.Server) error {
			if srv.Addr != ":8999" {
				t.Fatalf("addr = %q", srv.Addr)
			}
			return nil
		})

		cmd := commandWithWasteland("hop/wl-commons")
		cmd.Flags().Int("port", 8999, "")
		cmd.Flags().Bool("dev", false, "")
		_ = cmd.Flags().Set("local-db", "true")
		if err := runServe(cmd, io.Discard, io.Discard); err != nil {
			t.Fatalf("runServe(local) error = %v", err)
		}
		if !synced || !pushed {
			t.Fatalf("synced = %v pushed = %v", synced, pushed)
		}
	})

	t.Run("local sync failure", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			LocalDir:  t.TempDir(),
			RigHandle: "alice",
			Backend:   federation.BackendLocal,
			JoinedAt:  time.Now(),
		})
		installFakeDolt(t, "#!/bin/sh\nexit 0\n")
		withLocalWorkflowDBOverride(t, func(string, string) localWorkflowDB {
			return fakeLocalWorkflowDB{
				scriptedDB: scriptedDB{
					syncFunc: func() error { return fmt.Errorf("boom") },
				},
			}
		})

		cmd := commandWithWasteland("hop/wl-commons")
		cmd.Flags().Int("port", 8999, "")
		cmd.Flags().Bool("dev", false, "")
		_ = cmd.Flags().Set("local-db", "true")
		err := runServe(cmd, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "syncing with upstream: boom") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("remote missing token", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			RigHandle: "alice",
			Backend:   federation.BackendRemote,
			JoinedAt:  time.Now(),
		})
		t.Setenv("DOLTHUB_TOKEN", "")
		cmd := commandWithWasteland("hop/wl-commons")
		cmd.Flags().Int("port", 8999, "")
		cmd.Flags().Bool("dev", false, "")
		err := runServe(cmd, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "DOLTHUB_TOKEN required for remote mode") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("remote success", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			RigHandle: "alice",
			Backend:   federation.BackendRemote,
			JoinedAt:  time.Now(),
		})

		var synced bool
		withRemoteWorkflowDBOverride(t, func(token, upstreamOrg, upstreamDB, _, _, _ string) remoteWorkflowDB {
			if token != "token" || upstreamOrg != "hop" || upstreamDB != "wl-commons" {
				t.Fatalf("got %q %q %q", token, upstreamOrg, upstreamDB)
			}
			return fakeRemoteWorkflowDB{
				scriptedDB: scriptedDB{
					syncFunc: func() error {
						synced = true
						return nil
					},
					queryFunc: func(string, string) (string, error) { return "", nil },
				},
				diffFn: func(string) (string, error) { return "diff", nil },
			}
		})
		withServeListenOverride(t, func(srv *http.Server) error {
			if srv.Addr != ":8999" {
				t.Fatalf("addr = %q", srv.Addr)
			}
			return nil
		})

		cmd := commandWithWasteland("hop/wl-commons")
		cmd.Flags().Int("port", 8999, "")
		cmd.Flags().Bool("dev", false, "")
		if err := runServe(cmd, io.Discard, io.Discard); err != nil {
			t.Fatalf("runServe(remote) error = %v", err)
		}
		if !synced {
			t.Fatal("remote sync was not called")
		}
	})

	t.Run("remote sync warning", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("DOLTHUB_TOKEN", "token")
		saveTestConfig(t, &federation.Config{
			Upstream:  "hop/wl-commons",
			ForkOrg:   "alice",
			ForkDB:    "wl-commons",
			RigHandle: "alice",
			Backend:   federation.BackendRemote,
			JoinedAt:  time.Now(),
		})

		withRemoteWorkflowDBOverride(t, func(string, string, string, string, string, string) remoteWorkflowDB {
			return fakeRemoteWorkflowDB{
				scriptedDB: scriptedDB{
					syncFunc:  func() error { return fmt.Errorf("out of date") },
					queryFunc: func(string, string) (string, error) { return "", nil },
				},
			}
		})
		withServeListenOverride(t, func(*http.Server) error { return nil })

		cmd := commandWithWasteland("hop/wl-commons")
		cmd.Flags().Int("port", 8999, "")
		cmd.Flags().Bool("dev", false, "")
		if err := runServe(cmd, io.Discard, io.Discard); err != nil {
			t.Fatalf("runServe(remote warning) error = %v", err)
		}
	})

	t.Run("hosted env validation", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().Int("port", 8999, "")
		cmd.Flags().Bool("dev", false, "")
		t.Setenv("NANGO_SECRET_KEY", "")
		t.Setenv("WL_SESSION_SECRET", "")
		err := runServeHosted(cmd, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "NANGO_SECRET_KEY") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("hosted success", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().Int("port", 8124, "")
		cmd.Flags().Bool("dev", false, "")
		t.Setenv("NANGO_SECRET_KEY", "secret")
		t.Setenv("WL_SESSION_SECRET", "session")
		withHostedPublicDBOverride(t, func() commons.DB {
			return scriptedDB{
				queryFunc: func(string, string) (string, error) { return "", nil },
			}
		})
		withServeListenOverride(t, func(srv *http.Server) error {
			if srv.Addr != ":8124" {
				t.Fatalf("addr = %q", srv.Addr)
			}
			return nil
		})

		if err := runServeHosted(cmd, io.Discard, io.Discard); err != nil {
			t.Fatalf("runServeHosted() error = %v", err)
		}
	})
}

func TestListenAndServeGraceful_InvalidAddr(t *testing.T) {
	err := listenAndServeGraceful(&http.Server{Addr: "bad:addr"})
	if err == nil {
		t.Fatal("expected error")
	}
}
