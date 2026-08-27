//go:build live_e2e

package appserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLiveWindowsThreadArchiveCompat(t *testing.T) {
	if os.Getenv("CTR_GO_LIVE_ARCHIVE_COMPAT") != "1" {
		t.Skip("set CTR_GO_LIVE_ARCHIVE_COMPAT=1 to run")
	}
	if runtime.GOOS != "windows" {
		t.Skip("Windows compatibility path")
	}
	codexBin := strings.TrimSpace(os.Getenv("CTR_GO_CODEX_BIN"))
	if codexBin == "" {
		t.Fatal("CTR_GO_CODEX_BIN is required")
	}

	codexHome := t.TempDir()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	creator := NewClient(codexBin, "stdio://", workingDir, 30*time.Second, ClientOptions{CodexHome: codexHome})
	defer creator.Close()
	if err := creator.Start(ctx); err != nil {
		t.Fatalf("start creator App Server: %v", err)
	}

	statePath := filepath.Join(codexHome, "state_5.sqlite")
	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	threadID := "00000000-0000-7000-8000-000000000001"
	rolloutPath := filepath.Join(codexHome, "sessions", "2026", "08", "27", "rollout-2026-08-27T00-00-00-"+threadID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(rolloutPath), 0o755); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	metadata, err := json.Marshal(map[string]any{
		"timestamp": timestamp,
		"type":      "session_meta",
		"payload": map[string]any{
			"session_id":     threadID,
			"id":             threadID,
			"timestamp":      timestamp,
			"cwd":            workingDir,
			"originator":     "codex-tg live test",
			"cli_version":    "0.148.0",
			"source":         "vscode",
			"model_provider": "openai",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rolloutPath, append(metadata, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	prefixed := windowsExtendedPathPrefix + rolloutPath
	now := time.Now().Unix()
	if _, err := db.Exec(`
		INSERT INTO threads(
			id, rollout_path, created_at, updated_at, source, model_provider,
			cwd, title, sandbox_policy, approval_mode
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, threadID, prefixed, now, now, "vscode", "openai", workingDir,
		"archive compatibility live test", "danger-full-access", "never"); err != nil {
		t.Fatal(err)
	}
	if err := creator.Close(); err != nil {
		t.Fatalf("close creator App Server: %v", err)
	}

	restarted := NewClient(codexBin, "stdio://", workingDir, 30*time.Second, ClientOptions{CodexHome: codexHome})
	defer restarted.Close()
	if err := restarted.Start(ctx); err != nil {
		t.Fatalf("restart App Server: %v", err)
	}
	if got := rolloutPathForTest(t, db, threadID); got != rolloutPath {
		t.Fatalf("rollout path after App Server restart = %q, want %q", got, rolloutPath)
	}
	if _, err := restarted.ThreadResume(ctx, threadID, workingDir); err != nil {
		t.Fatalf("resume prefixed thread: %v", err)
	}
	if got := rolloutPathForTest(t, db, threadID); got != prefixed {
		t.Fatalf("rollout path after resume = %q, want App Server extended path %q", got, prefixed)
	}
	if !restarted.ThreadArchiveRequiresFreshSession(threadID) {
		t.Fatal("resumed Windows thread was not recorded in the fresh-archive cache")
	}
	if err := restarted.Close(); err != nil {
		t.Fatalf("close App Server holding resumed thread: %v", err)
	}

	admin := NewClient(codexBin, "stdio://", workingDir, 30*time.Second, ClientOptions{CodexHome: codexHome})
	defer admin.Close()
	if err := admin.Start(ctx); err != nil {
		t.Fatalf("start isolated archive App Server: %v", err)
	}
	if _, err := admin.ThreadArchive(ctx, threadID); err != nil {
		t.Fatalf("archive resumed prefixed thread: %v", err)
	}
	var archived int
	var archivedPath string
	if err := db.QueryRow(
		"SELECT archived, rollout_path FROM threads WHERE id = ?", threadID,
	).Scan(&archived, &archivedPath); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatalf("archived = %d, want 1", archived)
	}
	if _, err := os.Stat(archivedPath); err != nil {
		t.Fatalf("archived rollout does not exist: %v", err)
	}
}
