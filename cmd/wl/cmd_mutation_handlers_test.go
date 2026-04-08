package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/inference"
	"github.com/gastownhall/wasteland/internal/sdk"
)

func saveHandlerConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		RigHandle: "alice",
		Backend:   federation.BackendRemote,
		JoinedAt:  time.Now(),
	})
}

func withInferModelExistsOverride(t *testing.T, fn func(string) (bool, error)) {
	t.Helper()
	old := inferModelExists
	inferModelExists = fn
	t.Cleanup(func() {
		inferModelExists = old
	})
}

func withInferRunOverride(t *testing.T, fn func(*inference.Job) (*inference.Result, error)) {
	t.Helper()
	old := inferRun
	inferRun = fn
	t.Cleanup(func() {
		inferRun = old
	})
}

func withInferVerifyOverride(t *testing.T, fn func(*inference.Job, *inference.Result) (*inference.VerifyResult, error)) {
	t.Helper()
	old := inferVerify
	inferVerify = fn
	t.Cleanup(func() {
		inferVerify = old
	})
}

func TestRunClaim_PrintsMutationAndHint(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id + "-resolved", nil })

	var gotNoPush bool
	withCommandClientOverride(t, func(_ *federation.Config, noPush bool) (commandClient, error) {
		gotNoPush = noPush
		return fakeCommandClient{
			claimFn: func(wantedID string) (*sdk.MutationResult, error) {
				if wantedID != "w-123-resolved" {
					t.Fatalf("wantedID = %q", wantedID)
				}
				return &sdk.MutationResult{
					Detail: &sdk.DetailResult{
						Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "claimed"},
					},
				}, nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runClaim(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-123", true); err != nil {
		t.Fatalf("runClaim() error = %v", err)
	}
	if !gotNoPush {
		t.Fatal("noPush was not forwarded")
	}
	for _, want := range []string{"Claimed w-123-resolved", "Claimed by: alice", "wl done w-123-resolved"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q in %q", want, stdout.String())
		}
	}
}

func TestRunDone_PrintsEvidenceAndHint(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })
	withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
		return fakeCommandClient{
			doneFn: func(wantedID, evidence string) (*sdk.MutationResult, error) {
				if wantedID != "w-123" || evidence != "https://example/pr/1" {
					t.Fatalf("got %q %q", wantedID, evidence)
				}
				return &sdk.MutationResult{
					Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "in_review"}},
				}, nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runDone(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-123", "https://example/pr/1", false); err != nil {
		t.Fatalf("runDone() error = %v", err)
	}
	for _, want := range []string{"Completion submitted for w-123", "Evidence: https://example/pr/1", "Check: wl status w-123"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q in %q", want, stdout.String())
		}
	}
}

func TestRunAccept_ParsesInputsAndPrintsSummary(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })
	withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
		return fakeCommandClient{
			acceptFn: func(wantedID string, input sdk.AcceptInput) (*sdk.MutationResult, error) {
				if wantedID != "w-123" {
					t.Fatalf("wantedID = %q", wantedID)
				}
				if input.Reliability != 4 || input.Quality != 4 || input.Severity != "branch" {
					t.Fatalf("input = %+v", input)
				}
				if len(input.SkillTags) != 2 || input.SkillTags[1] != "auth" {
					t.Fatalf("skills = %v", input.SkillTags)
				}
				return &sdk.MutationResult{Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "completed"}}}, nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runAccept(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-123", 4, 0, "branch", "go, auth", "solid work", false); err != nil {
		t.Fatalf("runAccept() error = %v", err)
	}
	for _, want := range []string{"Accepted w-123", "Quality: 4, Reliability: 4", "Skills: go, auth", "Message: solid work"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q in %q", want, stdout.String())
		}
	}
}

func TestRunReject_Close_Unclaim_Delete_Update_Post(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	t.Run("reject", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				rejectFn: func(wantedID, reason string) (*sdk.MutationResult, error) {
					if reason != "tests failing" {
						t.Fatalf("reason = %q", reason)
					}
					return &sdk.MutationResult{Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "claimed"}}}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runReject(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-1", "tests failing", false); err != nil {
			t.Fatalf("runReject() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Reason: tests failing") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("close", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				closeFn: func(wantedID string) (*sdk.MutationResult, error) {
					return &sdk.MutationResult{Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "completed"}}}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runClose(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-2", false); err != nil {
			t.Fatalf("runClose() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Closed w-2") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("unclaim", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				unclaimFn: func(wantedID string) (*sdk.MutationResult, error) {
					return &sdk.MutationResult{Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "open"}}}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runUnclaim(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-3", false); err != nil {
			t.Fatalf("runUnclaim() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Unclaimed w-3") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("delete", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				deleteFn: func(_ string) (*sdk.MutationResult, error) {
					return &sdk.MutationResult{Branch: "wl/alice/w-4", Hint: "saved locally"}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runDelete(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-4", false); err != nil {
			t.Fatalf("runDelete() error = %v", err)
		}
		for _, want := range []string{"Withdrawn w-4", "Status: withdrawn", "Branch: wl/alice/w-4", "saved locally"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})

	t.Run("update", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				updateFn: func(wantedID string, fields *commons.WantedUpdate) (*sdk.MutationResult, error) {
					if wantedID != "w-5" || fields.Title != "New title" || !fields.TagsSet || len(fields.Tags) != 2 || fields.Tags[1] != "auth" {
						t.Fatalf("fields = %+v", fields)
					}
					return &sdk.MutationResult{Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: wantedID, Title: fields.Title, Status: "open"}}}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runUpdate(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-5", "New title", "Desc", "gastown", "bug", 1, "small", "go, auth", false); err != nil {
			t.Fatalf("runUpdate() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Updated w-5") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("post", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				postFn: func(input sdk.PostInput) (*sdk.MutationResult, error) {
					if input.Title != "Fix auth" || input.Type != "bug" || len(input.Tags) != 2 || input.Tags[0] != "go" {
						t.Fatalf("input = %+v", input)
					}
					return &sdk.MutationResult{
						Branch: "wl/alice/w-new",
						Hint:   "saved locally",
						Detail: &sdk.DetailResult{
							Item:  &commons.WantedItem{ID: "w-new", Title: input.Title, Status: "open"},
							PRURL: "https://example/pr/1",
						},
					}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runPost(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "Fix auth", "Desc", "gastown", "bug", 1, "small", "go, auth", false); err != nil {
			t.Fatalf("runPost() error = %v", err)
		}
		for _, want := range []string{"Posted wanted item: w-new", "Project:  gastown", "Type:     bug", "Tags:     go, auth", "PR: https://example/pr/1"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})
}

func TestRunAcceptUpstream_RejectUpstream_CloseUpstream(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	t.Run("accept-upstream", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				acceptUpstreamFn: func(wantedID, submitterHandle string, input sdk.AcceptInput) (*sdk.MutationResult, error) {
					if wantedID != "w-123" || submitterHandle != "charlie" {
						t.Fatalf("wantedID=%q submitter=%q", wantedID, submitterHandle)
					}
					if input.Quality != 4 || input.Reliability != 4 || input.Severity != "branch" {
						t.Fatalf("input = %+v", input)
					}
					if len(input.SkillTags) != 2 || input.SkillTags[0] != "go" || input.Message != "solid work" {
						t.Fatalf("input = %+v", input)
					}
					return &sdk.MutationResult{
						Detail: &sdk.DetailResult{
							Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "completed"},
						},
					}, nil
				},
			}, nil
		})

		var stdout bytes.Buffer
		if err := runAcceptUpstream(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-123", "charlie", 4, 0, "branch", "go, auth", "solid work", false); err != nil {
			t.Fatalf("runAcceptUpstream() error = %v", err)
		}
		for _, want := range []string{"Accepted upstream submission for w-123", "Submitter: charlie", "Quality: 4, Reliability: 4", "Skills: go, auth"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})

	t.Run("reject-upstream", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				rejectUpstreamFn: func(wantedID, submitterHandle string) error {
					if wantedID != "w-456" || submitterHandle != "dana" {
						t.Fatalf("wantedID=%q submitter=%q", wantedID, submitterHandle)
					}
					return nil
				},
			}, nil
		})

		var stdout bytes.Buffer
		if err := runRejectUpstream(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-456", "dana"); err != nil {
			t.Fatalf("runRejectUpstream() error = %v", err)
		}
		for _, want := range []string{"Rejected upstream submission for w-456", "Submitter: dana", "wl pending w-456"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})

	t.Run("close-upstream", func(t *testing.T) {
		var gotNoPush bool
		withCommandClientOverride(t, func(_ *federation.Config, noPush bool) (commandClient, error) {
			gotNoPush = noPush
			return fakeCommandClient{
				closeUpstreamFn: func(wantedID, submitterHandle string) (*sdk.MutationResult, error) {
					if wantedID != "w-789" || submitterHandle != "erin" {
						t.Fatalf("wantedID=%q submitter=%q", wantedID, submitterHandle)
					}
					return &sdk.MutationResult{
						Detail: &sdk.DetailResult{
							Item: &commons.WantedItem{ID: wantedID, Title: "Fix auth", Status: "completed"},
						},
					}, nil
				},
			}, nil
		})

		var stdout bytes.Buffer
		if err := runCloseUpstream(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-789", "erin", true); err != nil {
			t.Fatalf("runCloseUpstream() error = %v", err)
		}
		if !gotNoPush {
			t.Fatal("noPush was not forwarded")
		}
		for _, want := range []string{"Closed upstream submission for w-789", "Submitter: erin", "wl status w-789"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})
}

func TestRunClose_ErrorPaths(t *testing.T) {
	saveHandlerConfig(t)

	t.Run("resolve wanted error", func(t *testing.T) {
		withResolveWantedArgOverride(t, func(*federation.Config, string) (string, error) {
			return "", fmt.Errorf("bad wanted id")
		})
		err := runClose(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-bad", false)
		if err == nil || !strings.Contains(err.Error(), "bad wanted id") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("client close error", func(t *testing.T) {
		withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				closeFn: func(string) (*sdk.MutationResult, error) {
					return nil, fmt.Errorf("close failed")
				},
			}, nil
		})

		err := runClose(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-2", false)
		if err == nil || !strings.Contains(err.Error(), "close failed") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRunStatus_PrintsDetail(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })
	withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
		return fakeCommandClient{
			detailFn: func(wantedID string) (*sdk.DetailResult, error) {
				return &sdk.DetailResult{
					Item: &commons.WantedItem{
						ID:          wantedID,
						Title:       "Fix auth",
						Status:      "in_review",
						Type:        "bug",
						Priority:    1,
						EffortLevel: "small",
						Description: "Repair login",
						PostedBy:    "alice",
						ClaimedBy:   "bob",
					},
					Branch:     "wl/bob/w-123",
					BranchURL:  "https://example/branch",
					PRURL:      "https://example/pr/1",
					MainStatus: "open",
					Delta:      "claimed on branch",
					Completion: &commons.CompletionRecord{ID: "c-1", Evidence: "https://example/pr/1", CompletedBy: "bob"},
					Stamp:      &commons.Stamp{ID: "s-1", Quality: 4, Reliability: 4, Severity: "leaf", Author: "alice"},
				}, nil
			},
		}, nil
	})

	var stdout bytes.Buffer
	if err := runStatus(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-123"); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}
	for _, want := range []string{"w-123: Fix auth", "Branch:      wl/bob/w-123", "PR:          https://example/pr/1", "Completion:  c-1", "Stamp:       s-1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q in %q", want, stdout.String())
		}
	}
}

func TestRunInferPost_RunInferVerify_AndRunInferRun(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	t.Run("infer post", func(t *testing.T) {
		withInferModelExistsOverride(t, func(string) (bool, error) { return false, nil })
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				postFn: func(input sdk.PostInput) (*sdk.MutationResult, error) {
					job, err := inference.DecodeJob(input.Description)
					if err != nil {
						t.Fatalf("DecodeJob() error = %v", err)
					}
					if job.Model != "gpt-5" || input.Type != "inference" {
						t.Fatalf("input = %+v, job = %+v", input, job)
					}
					return &sdk.MutationResult{Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: "w-infer"}}, Hint: "posted"}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runInferPost(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "Summarize auth diff", "gpt-5", 7, 128, false); err != nil {
			t.Fatalf("runInferPost() error = %v", err)
		}
		for _, want := range []string{"model \"gpt-5\" not found", "Posted inference job: w-infer", "Next: wl infer run w-infer"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})

	t.Run("infer verify", func(t *testing.T) {
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
		var stdout bytes.Buffer
		if err := runInferVerify(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-infer"); err != nil {
			t.Fatalf("runInferVerify() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "VERIFIED") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("infer run success", func(t *testing.T) {
		jobJSON, err := inference.EncodeJob(&inference.Job{Prompt: "Summarize auth diff", Model: "gpt-5", Seed: 7})
		if err != nil {
			t.Fatalf("EncodeJob() error = %v", err)
		}
		withInferRunOverride(t, func(job *inference.Job) (*inference.Result, error) {
			if job.Model != "gpt-5" {
				t.Fatalf("job = %+v", job)
			}
			return &inference.Result{Output: "done", OutputHash: "abc", Model: job.Model, Seed: job.Seed}, nil
		})
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Title:       "Inference",
							Type:        "inference",
							Status:      "open",
							Description: jobJSON,
						},
					}, nil
				},
				claimFn: func(string) (*sdk.MutationResult, error) {
					return &sdk.MutationResult{}, nil
				},
				doneFn: func(wantedID, evidence string) (*sdk.MutationResult, error) {
					result, err := inference.DecodeResult(evidence)
					if err != nil {
						t.Fatalf("DecodeResult() error = %v", err)
					}
					if result.OutputHash != "abc" {
						t.Fatalf("result = %+v", result)
					}
					return &sdk.MutationResult{Detail: &sdk.DetailResult{Item: &commons.WantedItem{ID: wantedID}, PRURL: "https://example/pr/1"}}, nil
				},
			}, nil
		})
		var stdout bytes.Buffer
		if err := runInferRun(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-infer", false, false); err != nil {
			t.Fatalf("runInferRun() error = %v", err)
		}
		for _, want := range []string{"Inference completed for w-infer", "Completed by:  alice", "PR: https://example/pr/1"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})

	t.Run("infer run failure unclaims", func(t *testing.T) {
		jobJSON, err := inference.EncodeJob(&inference.Job{Prompt: "Summarize auth diff", Model: "gpt-5", Seed: 7})
		if err != nil {
			t.Fatalf("EncodeJob() error = %v", err)
		}
		withInferRunOverride(t, func(*inference.Job) (*inference.Result, error) {
			return nil, fmt.Errorf("ollama down")
		})
		unclaims := 0
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{ID: "w-infer", Type: "inference", Status: "open", Description: jobJSON},
					}, nil
				},
				claimFn: func(string) (*sdk.MutationResult, error) { return &sdk.MutationResult{}, nil },
				unclaimFn: func(string) (*sdk.MutationResult, error) {
					unclaims++
					return &sdk.MutationResult{}, nil
				},
			}, nil
		})
		err = runInferRun(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer", false, false)
		if err == nil || !strings.Contains(err.Error(), "running inference") {
			t.Fatalf("err = %v", err)
		}
		if unclaims != 1 {
			t.Fatalf("unclaims = %d", unclaims)
		}
	})
}

func TestRunInferStatus_ErrorPaths(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	t.Run("detail error", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return nil, fmt.Errorf("backend down")
				},
			}, nil
		})

		err := runInferStatus(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer")
		if err == nil || !strings.Contains(err.Error(), "querying wanted item: backend down") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("missing item", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{}, nil
				},
			}, nil
		})

		err := runInferStatus(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer")
		if err == nil || !strings.Contains(err.Error(), "wanted item w-infer not found") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRunInferVerify_ErrorAndMismatchPaths(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	t.Run("detail error", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return nil, fmt.Errorf("backend down")
				},
			}, nil
		})

		err := runInferVerify(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer")
		if err == nil || !strings.Contains(err.Error(), "querying wanted item: backend down") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("missing item", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{}, nil
				},
			}, nil
		})

		err := runInferVerify(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer")
		if err == nil || !strings.Contains(err.Error(), "wanted item w-infer not found") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("mismatch output", func(t *testing.T) {
		withInferVerifyOverride(t, func(_ *inference.Job, _ *inference.Result) (*inference.VerifyResult, error) {
			return &inference.VerifyResult{
				Match:        false,
				ExpectedHash: "expected",
				ActualHash:   "actual",
				Output:       "rerun output",
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
							Evidence: `{"output":"done","output_hash":"expected","model":"gpt-5","seed":7}`,
						},
					}, nil
				},
			}, nil
		})

		var stdout bytes.Buffer
		if err := runInferVerify(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-infer"); err != nil {
			t.Fatalf("runInferVerify() error = %v", err)
		}
		for _, want := range []string{"MISMATCH", "Expected: expected", "Actual:   actual", "Output:   rerun output"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})
}

func TestRunInferRun_ErrorAndSkipClaimPaths(t *testing.T) {
	saveHandlerConfig(t)
	withResolveWantedArgOverride(t, func(_ *federation.Config, id string) (string, error) { return id, nil })

	jobJSON, err := inference.EncodeJob(&inference.Job{Prompt: "Summarize auth diff", Model: "gpt-5", Seed: 7})
	if err != nil {
		t.Fatalf("EncodeJob() error = %v", err)
	}

	t.Run("skip claim requires claimed status", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Type:        "inference",
							Status:      "open",
							Description: jobJSON,
						},
					}, nil
				},
			}, nil
		})

		err := runInferRun(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer", false, true)
		if err == nil || !strings.Contains(err.Error(), `expected "claimed" (--skip-claim)`) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("claim error", func(t *testing.T) {
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Type:        "inference",
							Status:      "open",
							Description: jobJSON,
						},
					}, nil
				},
				claimFn: func(string) (*sdk.MutationResult, error) {
					return nil, fmt.Errorf("already claimed")
				},
			}, nil
		})

		err := runInferRun(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer", false, false)
		if err == nil || !strings.Contains(err.Error(), "claiming wanted item: already claimed") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("encode result error unclaims", func(t *testing.T) {
		withInferRunOverride(t, func(*inference.Job) (*inference.Result, error) {
			return &inference.Result{Output: "done", Model: "gpt-5", Seed: 7}, nil
		})

		unclaims := 0
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Type:        "inference",
							Status:      "open",
							Description: jobJSON,
						},
					}, nil
				},
				claimFn: func(string) (*sdk.MutationResult, error) { return &sdk.MutationResult{}, nil },
				unclaimFn: func(string) (*sdk.MutationResult, error) {
					unclaims++
					return &sdk.MutationResult{}, nil
				},
			}, nil
		})

		err := runInferRun(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer", false, false)
		if err == nil || !strings.Contains(err.Error(), "encoding inference result") {
			t.Fatalf("err = %v", err)
		}
		if unclaims != 1 {
			t.Fatalf("unclaims = %d", unclaims)
		}
	})

	t.Run("done error unclaims", func(t *testing.T) {
		withInferRunOverride(t, func(*inference.Job) (*inference.Result, error) {
			return &inference.Result{Output: "done", OutputHash: "abc", Model: "gpt-5", Seed: 7}, nil
		})

		unclaims := 0
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Type:        "inference",
							Status:      "open",
							Description: jobJSON,
						},
					}, nil
				},
				claimFn: func(string) (*sdk.MutationResult, error) { return &sdk.MutationResult{}, nil },
				doneFn: func(string, string) (*sdk.MutationResult, error) {
					return nil, fmt.Errorf("submit failed")
				},
				unclaimFn: func(string) (*sdk.MutationResult, error) {
					unclaims++
					return &sdk.MutationResult{}, nil
				},
			}, nil
		})

		err := runInferRun(commandWithWasteland("hop/wl-commons"), io.Discard, io.Discard, "w-infer", false, false)
		if err == nil || !strings.Contains(err.Error(), "submitting completion: submit failed") {
			t.Fatalf("err = %v", err)
		}
		if unclaims != 1 {
			t.Fatalf("unclaims = %d", unclaims)
		}
	})

	t.Run("skip claim success prints branch and hint", func(t *testing.T) {
		claims := 0
		unclaims := 0
		withInferRunOverride(t, func(*inference.Job) (*inference.Result, error) {
			return &inference.Result{Output: "done", OutputHash: "abc", Model: "gpt-5", Seed: 7}, nil
		})
		withCommandClientOverride(t, func(_ *federation.Config, _ bool) (commandClient, error) {
			return fakeCommandClient{
				detailFn: func(string) (*sdk.DetailResult, error) {
					return &sdk.DetailResult{
						Item: &commons.WantedItem{
							ID:          "w-infer",
							Type:        "inference",
							Status:      "claimed",
							Description: jobJSON,
						},
					}, nil
				},
				claimFn: func(string) (*sdk.MutationResult, error) {
					claims++
					return &sdk.MutationResult{}, nil
				},
				unclaimFn: func(string) (*sdk.MutationResult, error) {
					unclaims++
					return &sdk.MutationResult{}, nil
				},
				doneFn: func(_ string, evidence string) (*sdk.MutationResult, error) {
					result, err := inference.DecodeResult(evidence)
					if err != nil {
						t.Fatalf("DecodeResult() error = %v", err)
					}
					if result.OutputHash != "abc" {
						t.Fatalf("result = %+v", result)
					}
					return &sdk.MutationResult{
						Branch: "wl/alice/w-infer",
						Hint:   "review next",
					}, nil
				},
			}, nil
		})

		var stdout bytes.Buffer
		if err := runInferRun(commandWithWasteland("hop/wl-commons"), &stdout, io.Discard, "w-infer", false, true); err != nil {
			t.Fatalf("runInferRun() error = %v", err)
		}
		if claims != 0 || unclaims != 0 {
			t.Fatalf("claims = %d unclaims = %d", claims, unclaims)
		}
		for _, want := range []string{"Branch: wl/alice/w-infer", "review next", "Next: wl infer verify w-infer"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout missing %q in %q", want, stdout.String())
			}
		}
	})
}
