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

func (c *Client) preparePersistedThreadRolloutPaths() error {
	prepared, err := preparePersistedThreadRolloutState(runtime.GOOS, c.codexHome)
	if err != nil {
		return err
	}
	c.rolloutPathMu.Lock()
	defer c.rolloutPathMu.Unlock()
	c.preparedPaths = prepared
	c.resumedPaths = map[string]struct{}{}
	return nil
}

func (c *Client) prepareThreadRolloutPath(threadID string) error {
	if runtime.GOOS != "windows" || strings.TrimSpace(c.codexHome) == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}

	c.rolloutPathMu.Lock()
	defer c.rolloutPathMu.Unlock()
	if _, ok := c.preparedPaths[threadID]; ok {
		return nil
	}
	if err := prepareThreadRolloutState(runtime.GOOS, c.codexHome, threadID); err != nil {
		return err
	}
	if c.preparedPaths == nil {
		c.preparedPaths = map[string]struct{}{}
	}
	c.preparedPaths[threadID] = struct{}{}
	return nil
}

func (c *Client) forgetPreparedThreadRolloutPath(threadID string) {
	c.rolloutPathMu.Lock()
	defer c.rolloutPathMu.Unlock()
	delete(c.preparedPaths, threadID)
	delete(c.resumedPaths, threadID)
}

func (c *Client) markThreadRolloutResumed(threadID string) {
	c.rolloutPathMu.Lock()
	defer c.rolloutPathMu.Unlock()
	if c.resumedPaths == nil {
		c.resumedPaths = map[string]struct{}{}
	}
	c.resumedPaths[threadID] = struct{}{}
}

// ThreadArchiveRequiresFreshSession reports whether this App Server generation
// resumed the thread and therefore cached the Windows extended rollout path
// produced by Codex 0.148.0. The daemon uses this in-memory signal to recycle
// only an idle live generation before delegating archive back to App Server.
func (c *Client) ThreadArchiveRequiresFreshSession(threadID string) bool {
	if runtime.GOOS != "windows" || strings.TrimSpace(c.codexHome) == "" || strings.TrimSpace(threadID) == "" {
		return false
	}
	c.rolloutPathMu.Lock()
	defer c.rolloutPathMu.Unlock()
	_, ok := c.resumedPaths[threadID]
	return ok
}

// preparePersistedThreadRolloutState runs before the App Server process starts,
// so a fresh generation can archive persisted threads without first loading
// their Windows extended drive paths. A missing rollout is left unchanged so
// one stale thread cannot block startup; targeted preparation reports it if the
// thread is selected.
func preparePersistedThreadRolloutState(goos, codexHome string) (map[string]struct{}, error) {
	prepared := map[string]struct{}{}
	if goos != "windows" || strings.TrimSpace(codexHome) == "" {
		return prepared, nil
	}
	statePath := filepath.Join(codexHome, "state_5.sqlite")
	if _, err := os.Stat(statePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return prepared, nil
		}
		return nil, fmt.Errorf("inspect Codex state: %w", err)
	}

	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		return nil, fmt.Errorf("open Codex state: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("configure Codex state timeout: %w", err)
	}

	type threadPath struct {
		id      string
		current string
	}
	rows, err := db.Query(
		"SELECT id, rollout_path FROM threads WHERE archived = 0 AND substr(rollout_path, 1, ?) = ?",
		len(windowsExtendedPathPrefix),
		windowsExtendedPathPrefix,
	)
	if err != nil {
		return nil, fmt.Errorf("read persisted thread rollout paths: %w", err)
	}
	var paths []threadPath
	for rows.Next() {
		var path threadPath
		if err := rows.Scan(&path.id, &path.current); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan persisted thread rollout path: %w", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close persisted thread rollout paths: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate persisted thread rollout paths: %w", err)
	}

	for _, path := range paths {
		normalized, ok := normalizeWindowsExtendedDrivePath(path.current)
		if !ok {
			continue
		}
		if _, err := os.Stat(normalized); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("inspect persisted thread rollout: %w", err)
		}
		result, err := db.Exec(
			`UPDATE threads SET rollout_path = ? WHERE id = ? AND archived = 0 AND rollout_path = ?`,
			normalized,
			path.id,
			path.current,
		)
		if err != nil {
			return nil, fmt.Errorf("normalize persisted thread rollout path: %w", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("verify persisted thread rollout path: %w", err)
		}
		if updated == 1 {
			prepared[path.id] = struct{}{}
		}
	}
	return prepared, nil
}

// prepareThreadRolloutState works around a Codex App Server 0.148.0 Windows
// archive failure by normalizing the persisted path before resume or direct
// archive. App Server rewrites the prefix while resuming, so Client separately
// tracks resumed threads that require a fresh generation for archive. App
// Server remains authoritative for moving the rollout and updating state.
func prepareThreadRolloutState(goos, codexHome, threadID string) error {
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
