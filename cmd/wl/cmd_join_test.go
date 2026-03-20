package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
)

func TestGitConfigValue_MissingKey(t *testing.T) {
	t.Parallel()
	got := gitConfigValue("wasteland.nonexistent.key.12345")
	if got != "" {
		t.Errorf("gitConfigValue(missing) = %q, want empty string", got)
	}
}

func TestGitConfigValue_UserName(t *testing.T) {
	t.Parallel()
	// git config user.name may or may not be set in CI; just verify it doesn't panic
	_ = gitConfigValue("user.name")
}

func TestPrintForkInstructions(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	forkErr := &remote.ForkRequiredError{
		UpstreamOrg: "hop",
		UpstreamDB:  "wl-commons",
		ForkOrg:     "alice",
	}

	printForkInstructions(&buf, forkErr)
	got := buf.String()

	if !strings.Contains(got, "Fork required") {
		t.Errorf("output missing 'Fork required': %q", got)
	}
	if !strings.Contains(got, forkErr.ForkURL()) {
		t.Errorf("output missing fork URL %q: %q", forkErr.ForkURL(), got)
	}
	if !strings.Contains(got, "alice") {
		t.Errorf("output missing org name 'alice': %q", got)
	}
	if !strings.Contains(got, "wl join") {
		t.Errorf("output missing 'wl join': %q", got)
	}
}

func TestRunJoin_AdditionalProviderModes(t *testing.T) {
	tests := []struct {
		name          string
		upstream      string
		handle        string
		displayName   string
		email         string
		forkOrg       string
		remoteBase    string
		gitRemote     string
		github        bool
		githubLocal   string
		signed        bool
		direct        bool
		env           map[string]string
		wantType      string
		wantForkOrg   string
		wantHandle    string
		wantDirect    bool
		wantSigned    bool
		wantErrSubstr string
	}{
		{
			name:        "git provider",
			upstream:    "hop/wl-commons",
			displayName: "Alice",
			email:       "alice@example.com",
			forkOrg:     "alice",
			gitRemote:   "/tmp/git",
			wantType:    "git",
			wantForkOrg: "alice",
			wantHandle:  "alice",
		},
		{
			name:        "github local provider",
			upstream:    "hop/wl-commons",
			displayName: "Alice",
			email:       "alice@example.com",
			forkOrg:     "alice",
			githubLocal: "/tmp/github",
			// fake GitHub provider reports github to exercise that path.
			wantType:    "github",
			wantForkOrg: "alice",
			wantHandle:  "alice",
		},
		{
			name:        "github provider direct mode",
			upstream:    "hop/wl-commons",
			displayName: "Alice",
			email:       "alice@example.com",
			forkOrg:     "alice",
			github:      true,
			direct:      true,
			wantType:    "github",
			wantForkOrg: "alice",
			wantHandle:  "alice",
			wantDirect:  true,
		},
		{
			name:        "default dolthub provider",
			upstream:    "hop/wl-commons",
			displayName: "Alice",
			email:       "alice@example.com",
			env: map[string]string{
				"DOLTHUB_TOKEN": "token",
				"DOLTHUB_ORG":   "alice-org",
			},
			wantType:    "dolthub",
			wantForkOrg: "alice-org",
			wantHandle:  "alice-org",
		},
		{
			name:        "missing dolthub token",
			upstream:    "hop/wl-commons",
			displayName: "Alice",
			email:       "alice@example.com",
			env: map[string]string{
				"DOLTHUB_TOKEN": "",
				"DOLTHUB_ORG":   "alice-org",
			},
			wantErrSubstr: "DOLTHUB_TOKEN environment variable is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			installFakeDolt(t, "#!/bin/sh\nexit 0\n")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			var gotProviderType, gotForkOrg, gotHandle string
			var gotDirect bool
			withJoinWithProviderOverride(t, func(_ io.Writer, provider remote.Provider, _ federation.ConfigStore, upstream, forkOrg, handle, _, _, _ string, _ bool, direct bool) (*federation.JoinResult, error) {
				gotProviderType = provider.Type()
				gotForkOrg = forkOrg
				gotHandle = handle
				gotDirect = direct
				return &federation.JoinResult{
					Config: &federation.Config{
						Upstream:  upstream,
						ForkOrg:   forkOrg,
						ForkDB:    "wl-commons",
						LocalDir:  "/tmp/wl-commons",
						RigHandle: handle,
						JoinedAt:  time.Now(),
					},
				}, nil
			})

			var stdout bytes.Buffer
			err := runJoin(&stdout, io.Discard, tc.upstream, tc.handle, tc.displayName, tc.email, tc.forkOrg, tc.remoteBase, tc.gitRemote, tc.github, tc.githubLocal, tc.signed, tc.direct)
			if tc.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("runJoin() error = %v", err)
			}
			if gotProviderType != tc.wantType || gotForkOrg != tc.wantForkOrg || gotHandle != tc.wantHandle || gotDirect != tc.wantDirect {
				t.Fatalf("got type=%q forkOrg=%q handle=%q direct=%v", gotProviderType, gotForkOrg, gotHandle, gotDirect)
			}
		})
	}
}

func TestGPGKeyEmail_UsesSigningKeyAndLastUID(t *testing.T) {
	installFakeCommand(t, "git", `#!/bin/sh
set -eu
if [ "$1" = "config" ] && [ "$2" = "user.signingkey" ]; then
  printf 'ABC123\n'
  exit 0
fi
exit 1
`)
	installFakeCommand(t, "gpg", `#!/bin/sh
set -eu
printf 'sec::::::::::::::\n'
printf 'uid:::::::::Old Name <old@example.com>::\n'
printf 'uid:::::::::New Name <new@example.com>::\n'
`)

	if got := gpgKeyEmail(); got != "new@example.com" {
		t.Fatalf("gpgKeyEmail() = %q", got)
	}
}

func TestRunJoin_AlreadyJoinedFastPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, &federation.Config{
		Upstream:  "hop/wl-commons",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		LocalDir:  "/tmp/wl-commons",
		RigHandle: "alice",
		JoinedAt:  time.Now(),
	})
	installFakeDolt(t, "#!/bin/sh\nexit 0\n")

	var stdout bytes.Buffer
	err := runJoin(&stdout, io.Discard, "hop/wl-commons", "alice", "Alice", "alice@example.com", "alice", "/tmp/remotes", "", false, "", false, false)
	if err != nil {
		t.Fatalf("runJoin() error = %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "Already joined wasteland") || !strings.Contains(out, "Handle: alice") {
		t.Fatalf("output = %q", out)
	}
}

func TestRunJoin_SignedUsesGPGEmail(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	installFakeDolt(t, "#!/bin/sh\nexit 0\n")
	installFakeCommand(t, "git", `#!/bin/sh
set -eu
if [ "$1" = "config" ] && [ "$2" = "user.signingkey" ]; then
  exit 1
fi
exit 1
`)
	installFakeCommand(t, "gpg", `#!/bin/sh
set -eu
printf 'uid:::::::::Signer <signer@example.com>::\n'
`)

	var gotEmail string
	withJoinWithProviderOverride(t, func(_ io.Writer, _ remote.Provider, _ federation.ConfigStore, upstream, forkOrg, handle, _, email, _ string, _ bool, _ bool) (*federation.JoinResult, error) {
		gotEmail = email
		return &federation.JoinResult{
			Config: &federation.Config{
				Upstream:  upstream,
				ForkOrg:   forkOrg,
				ForkDB:    "wl-commons",
				LocalDir:  "/tmp/wl-commons",
				RigHandle: handle,
				JoinedAt:  time.Now(),
			},
		}, nil
	})

	var stdout bytes.Buffer
	err := runJoin(&stdout, io.Discard, "hop/wl-commons", "alice", "Alice", "", "alice", "/tmp/remotes", "", false, "", true, false)
	if err != nil {
		t.Fatalf("runJoin() error = %v", err)
	}
	if gotEmail != "signer@example.com" {
		t.Fatalf("email = %q", gotEmail)
	}
}
