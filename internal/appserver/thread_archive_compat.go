package appserver

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

const windowsExtendedPathPrefix = `\\?\`

func (c *Client) prepareThreadArchive(threadID string) error {
	return prepareThreadArchiveState(runtime.GOOS, c.codexHome, threadID)
}

// prepareThreadArchiveState works around a Codex App Server 0.148.0 Windows
// archive failure without taking ownership of the archive operation itself.
// App Server remains authoritative for moving the rollout and updating state.
func prepareThreadArchiveState(goos, codexHome, threadID string) error {
	if goos != "windows" || strings.TrimSpace(codexHome) == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}
	statePath := filepath.Join(codexHome, "state_5.sqlite")
	if _, err := os.Stat(statePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect Codex state: %w", err)
	}

	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		return fmt.Errorf("open Codex state: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return fmt.Errorf("configure Codex state timeout: %w", err)
	}

	var rolloutPath string
	err = db.QueryRow(
		"SELECT rollout_path FROM threads WHERE id = ? AND archived = 0",
		threadID,
	).Scan(&rolloutPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read thread rollout path: %w", err)
	}
	normalized, ok := normalizeWindowsExtendedDrivePath(rolloutPath)
	if !ok {
		return nil
	}
	if _, err := os.Stat(normalized); err != nil {
		return fmt.Errorf("inspect normalized thread rollout: %w", err)
	}

	result, err := db.Exec(
		`
		UPDATE threads
		SET rollout_path = ?
		WHERE id = ? AND archived = 0 AND rollout_path = ?
		`,
		normalized,
		threadID,
		rolloutPath,
	)
	if err != nil {
		return fmt.Errorf("normalize thread rollout path: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify normalized thread rollout path: %w", err)
	}
	if rows != 1 {
		return errors.New("thread rollout path changed during archive preparation")
	}
	return nil
}

func normalizeWindowsExtendedDrivePath(path string) (string, bool) {
	if len(path) < len(windowsExtendedPathPrefix)+3 || !strings.HasPrefix(path, windowsExtendedPathPrefix) {
		return "", false
	}
	drivePath := path[len(windowsExtendedPathPrefix):]
	if len(drivePath) < 3 || !isASCIIAlpha(drivePath[0]) || drivePath[1] != ':' || (drivePath[2] != '\\' && drivePath[2] != '/') {
		return "", false
	}
	return drivePath, true
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
