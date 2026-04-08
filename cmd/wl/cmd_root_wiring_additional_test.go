package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/inference"
	"github.com/gastownhall/wasteland/internal/pile"
	"github.com/gastownhall/wasteland/internal/remote"
	"github.com/gastownhall/wasteland/internal/sdk"
	"github.com/gastownhall/wasteland/internal/tui"
	"github.com/spf13/cobra"
)

func TestConfigCommand_WiringAndCompletions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})

	t.Run("config help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"config"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(config) = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "View or modify wasteland configuration settings.") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("config get and set", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"--wasteland", "hop/wl-commons", "config", "get", "mode"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(config get) = %d, stderr = %q", code, stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != "pr" {
			t.Fatalf("stdout = %q", stdout.String())
		}

		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"--wasteland", "hop/wl-commons", "config", "set", "mode", "wild-west"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(config set) = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "mode = wild-west") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	getCmd := newConfigGetCmd(io.Discard, io.Discard)
	items, directive := getCmd.ValidArgsFunction(getCmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || len(items) != 4 {
		t.Fatalf("get completions = %v, directive = %v", items, directive)
	}

	setCmd := newConfigSetCmd(io.Discard, io.Discard)
	items, directive = setCmd.ValidArgsFunction(setCmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || !strings.Contains(strings.Join(items, ","), "mode") {
		t.Fatalf("set key completions = %v, directive = %v", items, directive)
	}
	items, directive = setCmd.ValidArgsFunction(setCmd, []string{"mode"}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || strings.Join(items, ",") != "wild-west,pr" {
		t.Fatalf("set mode completions = %v, directive = %v", items, directive)
	}
	items, directive = setCmd.ValidArgsFunction(setCmd, []string{"signing"}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || strings.Join(items, ",") != "true,false" {
		t.Fatalf("set signing completions = %v, directive = %v", items, directive)
	}
}

func TestProfileCommand_Wiring(t *testing.T) {
	withPileOverrides(
		t,
		func() *pile.Client { return &pile.Client{} },
		nil,
		func(_ pile.RowQuerier, query string, limit int) ([]pile.ProfileSummary, error) {
			if query != "ali" || limit != 20 {
				t.Fatalf("query = %q limit = %d", query, limit)
			}
			return []pile.ProfileSummary{{Handle: "alice", DisplayName: "Alice Example"}}, nil
		},
	)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"profile", "--search", "ali"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(profile --search) = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Found 1 profiles:") || !strings.Contains(stdout.String(), "alice") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"profile"}, &stdout, &stderr); code != 1 {
		t.Fatalf("run(profile) = %d", code)
	}
	if !strings.Contains(stderr.String(), "provide a handle or use --search") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMeCommand_Wiring(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})

	withOpenDBFromConfigOverride(t, func(*federation.Config) (commons.DB, error) {
		return scriptedDB{
			queryFunc: func(query, _ string) (string, error) {
				switch {
				case strings.Contains(query, "claimed_by = 'alice' AND status IN ('claimed','in_review')"):
					return "id,title,status,priority,effort_level\nw-1,Fix auth,claimed,1,medium\n", nil
				case strings.Contains(query, "posted_by = 'alice' AND status = 'in_review'"):
					return "id,title,claimed_by\n", nil
				case strings.Contains(query, "claimed_by = 'alice' AND status = 'completed'"):
					return "id,title\n", nil
				default:
					return "", nil
				}
			},
		}, nil
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--wasteland", "hop/wl-commons", "me"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(me) = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Claimed items:") || !strings.Contains(stdout.String(), "w-1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestInferCommand_Wiring(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	t.Run("status", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Title:       "Inference",
							Status:      "completed",
							Type:        "inference",
							Description: `{"prompt":"Summarize auth diff","model":"gpt-5","seed":7}`,
						},
					}, nil
				},
			}, nil
		})

		var stdout, stderr bytes.Buffer
		if code := run([]string{"--wasteland", "hop/wl-commons", "infer", "status", "w-infer"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(infer status) = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "w-infer: Inference") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("verify", func(t *testing.T) {
		withInferVerifyOverride(t, func(_ *inference.Job, _ *inference.Result) (*inference.VerifyResult, error) {
			return &inference.VerifyResult{
				Match:        true,
				ExpectedHash: "abc",
				ActualHash:   "abc",
				Output:       "done",
			}, nil
		})
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Title:       "Inference",
							Type:        "inference",
							Description: `{"prompt":"Summarize auth diff","model":"gpt-5","seed":7}`,
						},
						Completion: &commons.CompletionRecord{
							Evidence: `{"output":"done","output_hash":"abc","model":"gpt-5","seed":7}`,
						},
					}, nil
				},
			}, nil
		})

		var stdout, stderr bytes.Buffer
		if code := run([]string{"--wasteland", "hop/wl-commons", "infer", "verify", "w-infer"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(infer verify) = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "VERIFIED") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})
}

func TestTUIAndServeCommand_Wiring(t *testing.T) {
	t.Run("tui", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
					syncFunc:  func() error { return nil },
					queryFunc: func(string, string) (string, error) { return "", nil },
				},
			}
		})
		withTeaOverrides(t,
			func(tui.Config) bubbletea.Model { return nil },
			func(bubbletea.Model, ...bubbletea.ProgramOption) teaProgram { return fakeTeaProgram{} },
		)

		var stdout, stderr bytes.Buffer
		if code := run([]string{"--wasteland", "hop/wl-commons", "tui"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(tui) = %d, stderr = %q", code, stderr.String())
		}
	})

	t.Run("serve self-hosted", func(t *testing.T) {
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
					syncFunc:  func() error { return nil },
					queryFunc: func(string, string) (string, error) { return "", nil },
				},
			}
		})
		withSelfHostedAPIServerOverride(t, func(client *sdk.Client) selfHostedAPIServer {
			return &fakeSelfHostedServer{client: client}
		})
		withServeListenOverride(t, func(*http.Server) error { return nil })

		var stdout, stderr bytes.Buffer
		if code := run([]string{"--wasteland", "hop/wl-commons", "serve", "--port", "8123"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(serve) = %d, stderr = %q", code, stderr.String())
		}
	})

	t.Run("serve hosted", func(t *testing.T) {
		t.Setenv("WL_SESSION_SECRET", "session")
		t.Setenv("WL_AUTH_SUBJECT_SECRET", "subject")
		t.Setenv("WL_ENVIRONMENT", "staging")
		t.Setenv("DOLTHUB_AUTH_BASE_URL", "https://auth.example")
		t.Setenv("DOLTHUB_AUTH_TENANT_ID", "tenant-dev")
		t.Setenv("DOLTHUB_AUTH_ENVIRONMENT", "staging")
		t.Setenv("DOLTHUB_AUTH_KEY_ID", "current-key")
		t.Setenv("DOLTHUB_AUTH_SHARED_SECRET", "current-secret")
		withHostedPublicDBOverride(t, func() commons.DB { return scriptedDB{} })
		withPendingWantedStatesOverride(t, func(string, string, string) (map[string][]remote.PendingWantedState, error) {
			return map[string][]remote.PendingWantedState{}, nil
		})
		withServeListenOverride(t, func(*http.Server) error { return nil })

		var stdout, stderr bytes.Buffer
		if code := run([]string{"serve", "--hosted", "--port", "8124"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(serve --hosted) = %d, stderr = %q", code, stderr.String())
		}
	})
}

func TestPostCommand_Wiring(t *testing.T) {
	saveHandlerConfig(t)
	cmd := newPostCmd(io.Discard, io.Discard)
	typeCompletion, ok := cmd.GetFlagCompletionFunc("type")
	if !ok {
		t.Fatal("missing type completion")
	}
	items, directive := typeCompletion(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || !strings.Contains(strings.Join(items, ","), "inference") {
		t.Fatalf("type completions = %v, directive = %v", items, directive)
	}
	effortCompletion, ok := cmd.GetFlagCompletionFunc("effort")
	if !ok {
		t.Fatal("missing effort completion")
	}
	items, directive = effortCompletion(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || strings.Join(items, ",") != "trivial,small,medium,large,epic" {
		t.Fatalf("effort completions = %v, directive = %v", items, directive)
	}
	withCommandClientOverride(t, func(_ *federation.Config, noPush bool) (commandClient, error) {
		if noPush {
			t.Fatal("noPush should default to false")
		}
		return fakeCommandClient{
			postFn: func(input sdk.PostInput) (*sdk.MutationResult, error) {
				if input.Title != "Fix auth" || input.Type != "bug" || input.Priority != 1 || input.EffortLevel != "small" {
					t.Fatalf("input = %+v", input)
				}
				return &sdk.MutationResult{
					Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: "w-123"}},
				}, nil
			},
		}, nil
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--wasteland", "hop/wl-commons",
		"post",
		"--title", "Fix auth",
		"--type", "bug",
		"--priority", "1",
		"--effort", "small",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(post) = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Posted wanted item: w-123") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
