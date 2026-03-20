package commons

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeDolt(t *testing.T, body string) (string, string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dolt"), []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake dolt: %v", err)
	}

	logPath := filepath.Join(root, "dolt.log")
	sqlLogPath := filepath.Join(root, "sql.log")
	dbDir := filepath.Join(root, "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("creating db dir: %v", err)
	}

	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOLT_LOG", logPath)
	t.Setenv("SQL_LOG", sqlLogPath)

	return dbDir, logPath, sqlLogPath
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestDoltEnvAndExecHelpers(t *testing.T) {
	dbDir, logPath, _ := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
exit 0
`)

	t.Setenv("DOLTHUB_TOKEN", "token-123")
	t.Setenv("DOLTHUB_ORG", "gastownhall")

	if got := DoltHubToken(); got != "token-123" {
		t.Fatalf("DoltHubToken() = %q, want token-123", got)
	}
	if got := DoltHubOrg(); got != "gastownhall" {
		t.Fatalf("DoltHubOrg() = %q, want gastownhall", got)
	}

	if err := FetchRemote(dbDir, "origin"); err != nil {
		t.Fatalf("FetchRemote() error = %v", err)
	}
	if err := PullUpstream(dbDir); err != nil {
		t.Fatalf("PullUpstream() error = %v", err)
	}
	if err := ResetMainToUpstream(dbDir); err != nil {
		t.Fatalf("ResetMainToUpstream() error = %v", err)
	}
	if err := CheckoutMain(dbDir); err != nil {
		t.Fatalf("CheckoutMain() error = %v", err)
	}

	logText := readTestFile(t, logPath)
	for _, want := range []string{
		"fetch origin",
		"pull upstream main",
		"fetch upstream",
		"reset --hard upstream/main",
		"checkout main",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("command log missing %q in %q", want, logText)
		}
	}
}

func TestDoltSQLBranchAndListHelpers(t *testing.T) {
	dbDir, _, sqlLogPath := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "sql" ] && [ "$2" = "--file" ]; then
  echo "---" >> "$SQL_LOG"
  cat "$3" >> "$SQL_LOG"
  exit 0
fi
if [ "$1" = "sql" ] && [ "$2" = "-r" ] && [ "$3" = "csv" ] && [ "$4" = "-q" ]; then
  query="$5"
  case "$query" in
    *"FROM dolt_branches WHERE name = 'wl/alice/w-1'"*)
      printf 'cnt\n1\n'
      ;;
    *"FROM dolt_remote_branches WHERE name = 'remotes/origin/wl/alice/w-1'"*)
      printf 'cnt\n1\n'
      ;;
    *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/%'"*)
      printf 'name\nwl/alice/w-1\nwl/bob/w-2\n'
      ;;
    *)
      printf 'value\nok\n'
      ;;
  esac
  exit 0
fi
exit 0
`)

	if err := DoltSQLScript(dbDir, "SELECT 1;"); err != nil {
		t.Fatalf("DoltSQLScript() error = %v", err)
	}
	if out, err := DoltSQLQuery(dbDir, "SELECT 1"); err != nil || out != "value\nok\n" {
		t.Fatalf("DoltSQLQuery() = %q, %v; want value/ok", out, err)
	}
	if exists, err := BranchExists(dbDir, "wl/alice/w-1"); err != nil || !exists {
		t.Fatalf("BranchExists() = %v, %v; want true, nil", exists, err)
	}
	if exists, err := RemoteBranchExists(dbDir, "remotes/origin/wl/alice/w-1"); err != nil || !exists {
		t.Fatalf("RemoteBranchExists() = %v, %v; want true, nil", exists, err)
	}
	branches, err := ListBranches(dbDir, "wl/")
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if got, want := strings.Join(branches, ","), "wl/alice/w-1,wl/bob/w-2"; got != want {
		t.Fatalf("ListBranches() = %q, want %q", got, want)
	}
	if err := DeleteBranch(dbDir, "wl/alice/w-stale"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}

	sqlLog := readTestFile(t, sqlLogPath)
	for _, want := range []string{
		"SELECT 1;",
		"CALL DOLT_BRANCH('-D', 'wl/alice/w-stale');",
	} {
		if !strings.Contains(sqlLog, want) {
			t.Fatalf("sql log missing %q in %q", want, sqlLog)
		}
	}
}

func TestCheckoutAndTrackOriginBranches(t *testing.T) {
	dbDir, logPath, sqlLogPath := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "sql" ] && [ "$2" = "--file" ]; then
  echo "---" >> "$SQL_LOG"
  cat "$3" >> "$SQL_LOG"
  exit 0
fi
if [ "$1" = "sql" ] && [ "$2" = "-r" ] && [ "$3" = "csv" ] && [ "$4" = "-q" ]; then
  query="$5"
  case "$query" in
    *"FROM dolt_branches WHERE name = 'wl/alice/w-1'"*)
      printf 'cnt\n0\n'
      ;;
    *"FROM dolt_branches WHERE name = 'wl/alice/w-2'"*)
      printf 'cnt\n0\n'
      ;;
    *"FROM dolt_remote_branches WHERE name = 'remotes/origin/wl/alice/w-1'"*)
      printf 'cnt\n1\n'
      ;;
    *"SELECT name FROM dolt_remote_branches WHERE name LIKE 'remotes/origin/wl/%'"*)
      printf 'name\nremotes/origin/wl/alice/w-1\nremotes/origin/wl/alice/w-remote\n'
      ;;
    *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/%'"*)
      printf 'name\nwl/alice/w-1\nwl/alice/w-stale\n'
      ;;
    *)
      printf 'cnt\n0\n'
      ;;
  esac
  exit 0
fi
exit 0
`)

	if err := CheckoutBranch(dbDir, "wl/alice/w-1"); err != nil {
		t.Fatalf("CheckoutBranch() error = %v", err)
	}
	if err := CheckoutBranchFrom(dbDir, "wl/alice/w-2", "upstream/main"); err != nil {
		t.Fatalf("CheckoutBranchFrom() error = %v", err)
	}
	if err := TrackOriginBranches(dbDir, "wl/"); err != nil {
		t.Fatalf("TrackOriginBranches() error = %v", err)
	}

	logText := readTestFile(t, logPath)
	for _, want := range []string{
		"branch wl/alice/w-1 remotes/origin/wl/alice/w-1",
		"checkout wl/alice/w-1",
		"branch wl/alice/w-2 upstream/main",
		"checkout wl/alice/w-2",
		"branch wl/alice/w-remote remotes/origin/wl/alice/w-remote",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("command log missing %q in %q", want, logText)
		}
	}

	sqlLog := readTestFile(t, sqlLogPath)
	if !strings.Contains(sqlLog, "CALL DOLT_BRANCH('-D', 'wl/alice/w-stale');") {
		t.Fatalf("expected stale branch delete in %q", sqlLog)
	}
}

func TestMergeAndPushHelpers(t *testing.T) {
	dbDir, logPath, sqlLogPath := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "sql" ] && [ "$2" = "--file" ]; then
  echo "---" >> "$SQL_LOG"
  cat "$3" >> "$SQL_LOG"
  exit 0
fi
exit 0
`)

	var stdout bytes.Buffer
	if err := MergeBranch(dbDir, "wl/alice/w-1"); err != nil {
		t.Fatalf("MergeBranch() error = %v", err)
	}
	if err := DeleteRemoteBranch(dbDir, "origin", "wl/alice/w-1"); err != nil {
		t.Fatalf("DeleteRemoteBranch() error = %v", err)
	}
	if err := PushBranch(dbDir, "wl/alice/w-1", &stdout); err != nil {
		t.Fatalf("PushBranch() error = %v", err)
	}
	if err := PushBranchToRemote(dbDir, "github", "wl/alice/w-2", &stdout); err != nil {
		t.Fatalf("PushBranchToRemote() error = %v", err)
	}
	if err := PushBranchToRemoteForce(dbDir, "origin", "main", true, &stdout); err != nil {
		t.Fatalf("PushBranchToRemoteForce() error = %v", err)
	}
	if err := PushOriginMain(dbDir, &stdout); err != nil {
		t.Fatalf("PushOriginMain() error = %v", err)
	}

	logText := readTestFile(t, logPath)
	for _, want := range []string{
		"push origin :wl/alice/w-1",
		"push --force origin wl/alice/w-1",
		"push github wl/alice/w-2",
		"push --force origin main",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("command log missing %q in %q", want, logText)
		}
	}

	sqlLog := readTestFile(t, sqlLogPath)
	if !strings.Contains(sqlLog, "CALL DOLT_MERGE('wl/alice/w-1');") {
		t.Fatalf("expected merge SQL in %q", sqlLog)
	}

	output := stdout.String()
	for _, want := range []string{
		"Pushed branch wl/alice/w-1 to origin",
		"Pushed branch wl/alice/w-2 to github",
		"Pushed branch main to origin",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q in %q", want, output)
		}
	}
}

func TestMergeBranch_ConflictAborts(t *testing.T) {
	dbDir, _, sqlLogPath := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "sql" ] && [ "$2" = "--file" ]; then
  script=$(cat "$3")
  echo "---" >> "$SQL_LOG"
  echo "$script" >> "$SQL_LOG"
  case "$script" in
    *"CALL DOLT_MERGE('--abort');"*)
      exit 0
      ;;
    *"CALL DOLT_MERGE('wl/alice/w-conflict');"*)
      echo "conflict detected" >&2
      exit 1
      ;;
  esac
fi
exit 0
`)

	err := MergeBranch(dbDir, "wl/alice/w-conflict")
	if err == nil {
		t.Fatal("expected merge conflict error")
	}
	if !strings.Contains(err.Error(), "merge conflict on branch wl/alice/w-conflict") {
		t.Fatalf("MergeBranch() error = %v, want conflict guidance", err)
	}

	sqlLog := readTestFile(t, sqlLogPath)
	if !strings.Contains(sqlLog, "CALL DOLT_MERGE('--abort');") {
		t.Fatalf("expected merge abort in %q", sqlLog)
	}
}

func TestPushWithSync_SyncsRejectedRemote(t *testing.T) {
	dbDir, logPath, _ := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "push" ] && [ "$2" = "origin" ] && [ "$3" = "main" ]; then
  count=0
  if [ -f "$COUNT_FILE" ]; then
    count=$(cat "$COUNT_FILE")
  fi
  count=$((count + 1))
  echo "$count" > "$COUNT_FILE"
  if [ "$count" -le 3 ]; then
    echo "stale ref" >&2
    exit 1
  fi
fi
exit 0
`)
	countPath := filepath.Join(filepath.Dir(logPath), "origin-push.count")
	t.Setenv("COUNT_FILE", countPath)

	var stdout bytes.Buffer
	if err := PushWithSync(dbDir, &stdout); err != nil {
		t.Fatalf("PushWithSync() error = %v", err)
	}

	logText := readTestFile(t, logPath)
	for _, want := range []string{
		"push upstream main",
		"push origin main",
		"pull origin main",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("command log missing %q in %q", want, logText)
		}
	}

	output := stdout.String()
	for _, want := range []string{
		"Syncing with origin",
		"Pushed to upstream",
		"Pushed to origin",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q in %q", want, output)
		}
	}
}

func TestEnsureGitHubRemote(t *testing.T) {
	t.Run("adds remote when missing", func(t *testing.T) {
		dbDir, logPath, _ := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "remote" ] && [ "$2" = "-v" ]; then
  printf 'origin file:///tmp/origin (fetch)\n'
  exit 0
fi
exit 0
`)

		if err := EnsureGitHubRemote(dbDir, "alice-dev", "wl-commons"); err != nil {
			t.Fatalf("EnsureGitHubRemote() error = %v", err)
		}

		logText := readTestFile(t, logPath)
		if !strings.Contains(logText, "remote add github https://github.com/alice-dev/wl-commons.git") {
			t.Fatalf("command log missing github add in %q", logText)
		}
	})

	t.Run("noops when github already exists", func(t *testing.T) {
		dbDir, logPath, _ := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "remote" ] && [ "$2" = "-v" ]; then
  printf 'github https://github.com/alice-dev/wl-commons.git (fetch)\n'
  exit 0
fi
exit 0
`)

		if err := EnsureGitHubRemote(dbDir, "alice-dev", "wl-commons"); err != nil {
			t.Fatalf("EnsureGitHubRemote() error = %v", err)
		}

		logText := readTestFile(t, logPath)
		if strings.Contains(logText, "remote add github") {
			t.Fatalf("did not expect github add in %q", logText)
		}
	})

	t.Run("tolerates already exists from add", func(t *testing.T) {
		dbDir, _, _ := writeFakeDolt(t, `#!/bin/sh
if [ "$1" = "remote" ] && [ "$2" = "-v" ]; then
  printf 'origin file:///tmp/origin (fetch)\n'
  exit 0
fi
if [ "$1" = "remote" ] && [ "$2" = "add" ]; then
  echo "already exists" >&2
  exit 1
fi
exit 0
`)

		if err := EnsureGitHubRemote(dbDir, "alice-dev", "wl-commons"); err != nil {
			t.Fatalf("EnsureGitHubRemote() error = %v, want nil", err)
		}
	})
}

func TestListResolveAndDetectItemHelpers(t *testing.T) {
	db := &operationsTestDB{
		queryFunc: func(sql, ref string) (string, error) {
			switch {
			case strings.Contains(sql, "ORDER BY created_at DESC LIMIT 50"):
				return "id\nw-2\nw-1\n", nil
			case strings.Contains(sql, "LIKE 'w-1%'"):
				return "id\nw-123\n", nil
			case strings.Contains(sql, "LIKE 'w-%'"):
				return "id\nw-1\nw-2\n", nil
			case strings.Contains(sql, "WHERE id = 'missing'"):
				return "status\n", nil
			case strings.Contains(sql, "WHERE id = 'w-1'") && ref == "":
				return "status\nclaimed\n", nil
			case strings.Contains(sql, "WHERE id = 'w-1'") && ref == "origin/main":
				return "status\nopen\n", nil
			case strings.Contains(sql, "WHERE id = 'w-1'") && ref == "upstream/main":
				return "status\nin_review\n", nil
			default:
				return "", nil
			}
		},
	}

	ids, err := ListWantedIDs(db, "claimed")
	if err != nil {
		t.Fatalf("ListWantedIDs() error = %v", err)
	}
	if got, want := strings.Join(ids, ","), "w-2,w-1"; got != want {
		t.Fatalf("ListWantedIDs() = %q, want %q", got, want)
	}

	fullID, err := ResolveWantedID(db, "w-1")
	if err != nil {
		t.Fatalf("ResolveWantedID() error = %v", err)
	}
	if fullID != "w-123" {
		t.Fatalf("ResolveWantedID() = %q, want w-123", fullID)
	}

	if status := QueryItemStatusAsOf(db, "w-1", "origin/main"); status != "open" {
		t.Fatalf("QueryItemStatusAsOf() = %q, want open", status)
	}
	if status := QueryItemStatusAsOf(db, "missing", "origin/main"); status != "" {
		t.Fatalf("QueryItemStatusAsOf() = %q, want empty for missing item", status)
	}

	dbDir, _, _ := writeFakeDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
exit 0
`)
	loc, err := DetectItemLocation(dbDir, db, "w-1")
	if err != nil {
		t.Fatalf("DetectItemLocation() error = %v", err)
	}
	if !loc.FetchedOrigin || !loc.FetchedUpstream {
		t.Fatalf("DetectItemLocation() fetched flags = %+v, want both true", loc)
	}
	if loc.LocalStatus != "claimed" || loc.OriginStatus != "open" || loc.UpstreamStatus != "in_review" {
		t.Fatalf("DetectItemLocation() = %+v", loc)
	}
}

func TestResolveWantedID_Errors(t *testing.T) {
	db := &operationsTestDB{
		queryFunc: func(sql, _ string) (string, error) {
			switch {
			case strings.Contains(sql, "LIKE 'none%'"):
				return "id\n", nil
			case strings.Contains(sql, "LIKE 'amb%'"):
				return "id\namb-1\namb-2\n", nil
			default:
				return "", nil
			}
		},
	}

	if _, err := ResolveWantedID(db, "none"); err == nil || !strings.Contains(err.Error(), `no wanted item matching "none"`) {
		t.Fatalf("ResolveWantedID(none) error = %v, want no-match error", err)
	}
	if _, err := ResolveWantedID(db, "amb"); err == nil || !strings.Contains(err.Error(), `ambiguous prefix "amb" matches: amb-1, amb-2`) {
		t.Fatalf("ResolveWantedID(amb) error = %v, want ambiguous error", err)
	}
}
