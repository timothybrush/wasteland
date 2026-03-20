package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeBackendDolt(t *testing.T, body string) (string, string, string) {
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

func readBackendTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestLocalDB_QueryWrappersAndSync(t *testing.T) {
	dbDir, logPath, sqlLogPath := writeFakeBackendDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "sql" ] && [ "$2" = "--file" ]; then
  echo "---" >> "$SQL_LOG"
  cat "$3" >> "$SQL_LOG"
  exit 0
fi
if [ "$1" = "sql" ] && [ "$2" = "-r" ] && [ "$3" = "csv" ] && [ "$4" = "-q" ]; then
  query="$5"
  case "$query" in
    *"SELECT name FROM dolt_branches WHERE name LIKE 'wl/%'"*)
      printf 'name\nwl/alice/w-1\n'
      ;;
    *"SELECT name FROM dolt_remote_branches WHERE name LIKE 'remotes/origin/wl/%'"*)
      printf 'name\n'
      ;;
    *)
      printf 'id\nw-1\n'
      ;;
  esac
  exit 0
fi
exit 0
`)

	db := NewLocalDB(dbDir, "pr")
	if db.Dir() != dbDir {
		t.Fatalf("Dir() = %q, want %q", db.Dir(), dbDir)
	}
	if _, err := db.Query("SELECT id FROM wanted", "origin/main"); err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	branches, err := db.Branches("wl/")
	if err != nil {
		t.Fatalf("Branches() error = %v", err)
	}
	if len(branches) != 1 || branches[0] != "wl/alice/w-1" {
		t.Fatalf("Branches() = %+v, want wl/alice/w-1", branches)
	}
	if err := db.DeleteBranch("wl/alice/w-1"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if err := db.DeleteRemoteBranch("wl/alice/w-2"); err != nil {
		t.Fatalf("DeleteRemoteBranch() error = %v", err)
	}
	if err := db.PushBranch("wl/alice/w-3", os.Stdout); err != nil {
		t.Fatalf("PushBranch() error = %v", err)
	}
	if err := db.PushMain(os.Stdout); err != nil {
		t.Fatalf("PushMain() error = %v", err)
	}
	if err := db.PushWithSync(os.Stdout); err != nil {
		t.Fatalf("PushWithSync() error = %v", err)
	}
	if err := db.MergeBranch("wl/alice/w-4"); err != nil {
		t.Fatalf("MergeBranch() error = %v", err)
	}
	if err := db.CanWildWest(); err != nil {
		t.Fatalf("CanWildWest() error = %v", err)
	}
	if err := db.Sync(); err != nil {
		t.Fatalf("Sync(pr) error = %v", err)
	}

	dbWW := NewLocalDB(dbDir, "wild-west")
	if err := dbWW.Sync(); err != nil {
		t.Fatalf("Sync(wild-west) error = %v", err)
	}

	logText := readBackendTestFile(t, logPath)
	for _, want := range []string{
		"sql -r csv -q SELECT id FROM wanted AS OF 'origin/main'",
		"push origin :wl/alice/w-2",
		"push --force origin wl/alice/w-3",
		"push --force origin main",
		"push upstream main",
		"fetch upstream",
		"reset --hard upstream/main",
		"fetch origin",
		"pull upstream main",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("command log missing %q in %q", want, logText)
		}
	}

	sqlLog := readBackendTestFile(t, sqlLogPath)
	for _, want := range []string{
		"CALL DOLT_BRANCH('-D', 'wl/alice/w-1');",
		"CALL DOLT_MERGE('wl/alice/w-4');",
	} {
		if !strings.Contains(sqlLog, want) {
			t.Fatalf("sql log missing %q in %q", want, sqlLog)
		}
	}
}

func TestLocalDB_ExecBuildsCommitScriptAndRestoresMain(t *testing.T) {
	dbDir, logPath, sqlLogPath := writeFakeBackendDolt(t, `#!/bin/sh
echo "$@" >> "$DOLT_LOG"
if [ "$1" = "sql" ] && [ "$2" = "--file" ]; then
  echo "---" >> "$SQL_LOG"
  cat "$3" >> "$SQL_LOG"
  exit 0
fi
if [ "$1" = "sql" ] && [ "$2" = "-r" ] && [ "$3" = "csv" ] && [ "$4" = "-q" ]; then
  printf 'cnt\n0\n'
  exit 0
fi
exit 0
`)

	db := NewLocalDB(dbDir, "pr")
	if err := db.Exec("wl/alice/w-1", "wl claim: w-1", true, "UPDATE wanted SET status='claimed' WHERE id='w-1'"); err != nil {
		t.Fatalf("Exec(branch) error = %v", err)
	}
	if err := db.Exec("", "wl update: w-1", false, "UPDATE wanted SET title='New title' WHERE id='w-1'"); err != nil {
		t.Fatalf("Exec(main) error = %v", err)
	}

	logText := readBackendTestFile(t, logPath)
	for _, want := range []string{
		"branch wl/alice/w-1 main",
		"checkout wl/alice/w-1",
		"checkout main",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("command log missing %q in %q", want, logText)
		}
	}

	sqlLog := readBackendTestFile(t, sqlLogPath)
	for _, want := range []string{
		"UPDATE wanted SET status='claimed' WHERE id='w-1';",
		"CALL DOLT_ADD('-A');",
		"CALL DOLT_COMMIT('-S', '-m', 'wl claim: w-1');",
		"CALL DOLT_COMMIT('-m', 'wl update: w-1');",
	} {
		if !strings.Contains(sqlLog, want) {
			t.Fatalf("sql log missing %q in %q", want, sqlLog)
		}
	}
}

func TestInjectAsOfAndExtractTableName(t *testing.T) {
	if got := extractTableName("wanted WHERE status='open'"); got != "wanted" {
		t.Fatalf("extractTableName() = %q, want wanted", got)
	}
	if got := injectAsOf("SELECT id FROM wanted WHERE status='open'", "origin/main"); got != "SELECT id FROM wanted AS OF 'origin/main' WHERE status='open'" {
		t.Fatalf("injectAsOf() = %q", got)
	}
	if got := injectAsOf("SELECT 1", "origin/main"); got != "SELECT 1" {
		t.Fatalf("injectAsOf(no from) = %q, want unchanged query", got)
	}
}
