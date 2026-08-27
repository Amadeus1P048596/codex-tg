package appserver

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestThreadArchivePreparesWindowsStateBeforeRPC(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows compatibility path")
	}
	home := t.TempDir()
	rollout := filepath.Join(home, "sessions", "rollout-thread-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollout, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := createArchiveCompatStateDB(t, home, "thread-1", `\\?\`+rollout)
	client := NewClient("codex", "stdio://", t.TempDir(), time.Second, ClientOptions{CodexHome: home})

	_, err := client.ThreadArchive(context.Background(), "thread-1")
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("ThreadArchive error = %v, want not-running RPC error", err)
	}
	if got := rolloutPathForTest(t, db, "thread-1"); got != rollout {
		t.Fatalf("rollout_path = %q, want %q before RPC", got, rollout)
	}
}

func TestPrepareThreadArchiveStateNormalizesWindowsExtendedPath(t *testing.T) {
	home := t.TempDir()
	rollout := filepath.Join(home, "sessions", "rollout-thread-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollout, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := createArchiveCompatStateDB(t, home, "thread-1", `\\?\`+rollout)

	if err := prepareThreadArchiveState("windows", home, "thread-1"); err != nil {
		t.Fatalf("prepareThreadArchiveState failed: %v", err)
	}
	if got := rolloutPathForTest(t, db, "thread-1"); got != rollout {
		t.Fatalf("rollout_path = %q, want %q", got, rollout)
	}
}

func TestPrepareThreadArchiveStateLeavesOrdinaryAndNonWindowsPathsAlone(t *testing.T) {
	t.Run("ordinary Windows path", func(t *testing.T) {
		home := t.TempDir()
		rollout := filepath.Join(home, "sessions", "rollout-thread-1.jsonl")
		db := createArchiveCompatStateDB(t, home, "thread-1", rollout)
		if err := prepareThreadArchiveState("windows", home, "thread-1"); err != nil {
			t.Fatalf("prepareThreadArchiveState failed: %v", err)
		}
		if got := rolloutPathForTest(t, db, "thread-1"); got != rollout {
			t.Fatalf("rollout_path = %q, want unchanged %q", got, rollout)
		}
	})

	t.Run("non-Windows", func(t *testing.T) {
		home := t.TempDir()
		rollout := filepath.Join(home, "sessions", "rollout-thread-1.jsonl")
		prefixed := `\\?\` + rollout
		db := createArchiveCompatStateDB(t, home, "thread-1", prefixed)
		if err := prepareThreadArchiveState("linux", home, "thread-1"); err != nil {
			t.Fatalf("prepareThreadArchiveState failed: %v", err)
		}
		if got := rolloutPathForTest(t, db, "thread-1"); got != prefixed {
			t.Fatalf("rollout_path = %q, want unchanged %q", got, prefixed)
		}
	})
}

func TestPrepareThreadArchiveStateFailsClosedWhenNormalizedRolloutIsMissing(t *testing.T) {
	home := t.TempDir()
	missing := filepath.Join(home, "sessions", "missing.jsonl")
	db := createArchiveCompatStateDB(t, home, "thread-1", `\\?\`+missing)

	if err := prepareThreadArchiveState("windows", home, "thread-1"); err == nil {
		t.Fatal("prepareThreadArchiveState succeeded, want missing rollout error")
	}
	if got := rolloutPathForTest(t, db, "thread-1"); got != `\\?\`+missing {
		t.Fatalf("rollout_path = %q, want original prefixed path", got)
	}
}

func TestPrepareThreadArchiveStateAllowsMissingHomeState(t *testing.T) {
	if err := prepareThreadArchiveState("windows", "", "thread-1"); err != nil {
		t.Fatalf("empty home should be a no-op: %v", err)
	}
	if err := prepareThreadArchiveState("windows", t.TempDir(), "thread-1"); err != nil {
		t.Fatalf("missing state database should be a no-op: %v", err)
	}
}

func TestNormalizeWindowsExtendedDrivePathRejectsUNCAndRelativePaths(t *testing.T) {
	for _, path := range []string{
		`\\?\UNC\server\share\rollout.jsonl`,
		`\\?\sessions\rollout.jsonl`,
		`C:\sessions\rollout.jsonl`,
	} {
		if got, ok := normalizeWindowsExtendedDrivePath(path); ok || got != "" {
			t.Fatalf("normalizeWindowsExtendedDrivePath(%q) = %q, %v; want rejected", path, got, ok)
		}
	}
}

func createArchiveCompatStateDB(t *testing.T, home, threadID, rolloutPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		rollout_path TEXT NOT NULL,
		archived INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO threads(id, rollout_path, archived) VALUES (?, ?, 0)",
		threadID,
		rolloutPath,
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func rolloutPathForTest(t *testing.T, db *sql.DB, threadID string) string {
	t.Helper()
	var got string
	if err := db.QueryRow(
		"SELECT rollout_path FROM threads WHERE id = ?", threadID,
	).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}
