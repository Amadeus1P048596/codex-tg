package codexruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const stateVersion = 1

type Manager struct {
	Root        string
	SourceRoots []string
	Probe       func(string) error
}

type Result struct {
	Path            string
	PreviousPath    string
	Version         string
	Source          string
	Promoted        bool
	NeedsValidation bool
}

type state struct {
	Version           int    `json:"version"`
	Active            string `json:"active"`
	Previous          string `json:"previous,omitempty"`
	ActiveConfirmed   bool   `json:"active_confirmed"`
	PreviousConfirmed bool   `json:"previous_confirmed,omitempty"`
	Updated           string `json:"updated_at"`
}

type candidate struct {
	dir     string
	modTime time.Time
}

func New(root string, sourceRoots []string) *Manager {
	return &Manager{
		Root:        filepath.Clean(root),
		SourceRoots: append([]string(nil), sourceRoots...),
		Probe:       probeExecutable,
	}
}

func DefaultSourceRoots() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return nil
	}
	return []string{filepath.Join(localAppData, "OpenAI", "Codex", "bin")}
}

func RequiredFiles() []string {
	if runtime.GOOS != "windows" {
		return []string{executableName()}
	}
	return []string{
		"codex.exe",
		"codex-code-mode-host.exe",
		"codex-command-runner.exe",
		"codex-windows-sandbox-setup.exe",
	}
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "codex.exe"
	}
	return "codex"
}

// Ensure resolves a private, complete Codex runtime. A valid active copy wins
// even when a Desktop update has already removed the configured source path.
func (m *Manager) Ensure(configured string) (Result, error) {
	if err := m.validate(); err != nil {
		return Result{}, err
	}
	current, err := m.loadState()
	if err == nil {
		if result, ok := m.resultFor(current.Active, current.Previous, "active", false, !current.ActiveConfirmed); ok {
			return result, nil
		}
		if result, ok := m.resultFor(current.Previous, current.Active, "previous", false, !current.PreviousConfirmed); ok {
			current.Active, current.Previous = current.Previous, current.Active
			current.ActiveConfirmed, current.PreviousConfirmed = current.PreviousConfirmed, current.ActiveConfirmed
			if writeErr := m.writeState(current); writeErr != nil {
				return Result{}, writeErr
			}
			return result, nil
		}
	}

	best, findErr := m.bestCandidate(configured)
	if findErr != nil {
		return Result{}, findErr
	}
	version, path, installErr := m.install(best.dir)
	if installErr != nil {
		return Result{}, installErr
	}
	next := state{Version: stateVersion, Active: version, Updated: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := m.writeState(next); err != nil {
		return Result{}, err
	}
	m.pruneUnused(next.Active, next.Previous)
	return Result{Path: path, Version: version, Source: "bootstrap", Promoted: true, NeedsValidation: true}, nil
}

// Current inspects the managed runtime without copying files or changing state.
func (m *Manager) Current() (Result, error) {
	if err := m.validate(); err != nil {
		return Result{}, err
	}
	current, err := m.loadState()
	if err != nil {
		return Result{}, err
	}
	if result, ok := m.resultFor(current.Active, current.Previous, "active", false, !current.ActiveConfirmed); ok {
		return result, nil
	}
	if result, ok := m.resultFor(current.Previous, current.Active, "previous", false, !current.PreviousConfirmed); ok {
		return result, nil
	}
	return Result{}, errors.New("managed Codex runtime has no complete active or previous bundle")
}

// Sync stages the newest complete source bundle and promotes it for the next
// daemon start. The currently running App Server processes are not disturbed.
func (m *Manager) Sync(configured string) (Result, error) {
	active, err := m.Ensure(configured)
	if err != nil {
		return Result{}, err
	}
	current, err := m.loadState()
	if err != nil {
		return Result{}, err
	}
	best, err := m.bestCandidate(configured)
	if err != nil {
		return active, nil
	}
	wanted, err := versionID(best.dir)
	if err != nil {
		return Result{}, err
	}
	if wanted == current.Active {
		return active, nil
	}
	version, path, err := m.install(best.dir)
	if err != nil {
		return Result{}, err
	}
	if version == current.Active {
		return active, nil
	}
	next := state{
		Version:           stateVersion,
		Active:            version,
		Previous:          current.Active,
		ActiveConfirmed:   false,
		PreviousConfirmed: current.ActiveConfirmed,
		Updated:           time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := m.writeState(next); err != nil {
		return Result{}, err
	}
	m.pruneUnused(next.Active, next.Previous)
	return Result{
		Path:            path,
		PreviousPath:    m.executablePath(current.Active),
		Version:         version,
		Source:          "sync",
		Promoted:        true,
		NeedsValidation: true,
	}, nil
}

func (m *Manager) Confirm(version string) error {
	current, err := m.loadState()
	if err != nil {
		return err
	}
	if strings.TrimSpace(version) == "" || version != current.Active {
		return errors.New("cannot confirm a non-active Codex runtime")
	}
	current.ActiveConfirmed = true
	if err := m.writeState(current); err != nil {
		return err
	}
	m.pruneUnused(current.Active, current.Previous)
	return nil
}

func (m *Manager) Rollback() (Result, error) {
	current, err := m.loadState()
	if err != nil {
		return Result{}, err
	}
	if current.Previous == "" {
		return Result{}, errors.New("no previous Codex runtime is available")
	}
	if !completeBundle(m.versionDir(current.Previous)) {
		return Result{}, fmt.Errorf("previous Codex runtime %q is incomplete", current.Previous)
	}
	next := state{
		Version:           stateVersion,
		Active:            current.Previous,
		Previous:          current.Active,
		ActiveConfirmed:   current.PreviousConfirmed,
		PreviousConfirmed: current.ActiveConfirmed,
		Updated:           time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := m.writeState(next); err != nil {
		return Result{}, err
	}
	m.pruneUnused(next.Active, next.Previous)
	return Result{
		Path:            m.executablePath(next.Active),
		PreviousPath:    m.executablePath(next.Previous),
		Version:         next.Active,
		Source:          "rollback",
		Promoted:        true,
		NeedsValidation: !next.ActiveConfirmed,
	}, nil
}

func (m *Manager) validate() error {
	if strings.TrimSpace(m.Root) == "" || m.Root == "." {
		return errors.New("Codex runtime root is required")
	}
	return nil
}

func (m *Manager) resultFor(active, previous, source string, promoted, needsValidation bool) (Result, bool) {
	if active == "" || !completeBundle(m.versionDir(active)) {
		return Result{}, false
	}
	result := Result{
		Path:            m.executablePath(active),
		Version:         active,
		Source:          source,
		Promoted:        promoted,
		NeedsValidation: needsValidation,
	}
	if previous != "" && completeBundle(m.versionDir(previous)) {
		result.PreviousPath = m.executablePath(previous)
	}
	return result, true
}

func (m *Manager) bestCandidate(configured string) (candidate, error) {
	seen := map[string]struct{}{}
	var candidates []candidate
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		dir = filepath.Clean(dir)
		key := strings.ToLower(dir)
		if _, ok := seen[key]; ok || !completeBundle(dir) {
			return
		}
		seen[key] = struct{}{}
		info, err := os.Stat(filepath.Join(dir, executableName()))
		if err != nil {
			return
		}
		candidates = append(candidates, candidate{dir: dir, modTime: info.ModTime()})
	}
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if resolved, err := exec.LookPath(configured); err == nil {
			configured = resolved
		}
		add(filepath.Dir(configured))
	}
	for _, root := range m.SourceRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				add(filepath.Join(root, entry.Name()))
			}
		}
	}
	if len(candidates) == 0 {
		return candidate{}, fmt.Errorf("no complete Codex runtime found; required files: %s", strings.Join(RequiredFiles(), ", "))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0], nil
}

func (m *Manager) install(source string) (string, string, error) {
	version, err := versionID(source)
	if err != nil {
		return "", "", err
	}
	destination := m.versionDir(version)
	if completeBundle(destination) {
		if err := m.probe(filepath.Join(destination, executableName())); err != nil {
			return "", "", err
		}
		return version, filepath.Join(destination, executableName()), nil
	}
	if err := os.MkdirAll(filepath.Join(m.Root, "versions"), 0o755); err != nil {
		return "", "", err
	}
	staging := destination + ".staging"
	if err := os.RemoveAll(staging); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", "", err
	}
	for _, name := range RequiredFiles() {
		if err := copyFile(filepath.Join(source, name), filepath.Join(staging, name)); err != nil {
			_ = os.RemoveAll(staging)
			return "", "", err
		}
	}
	if !completeBundle(staging) {
		_ = os.RemoveAll(staging)
		return "", "", errors.New("staged Codex runtime is incomplete")
	}
	if err := m.probe(filepath.Join(staging, executableName())); err != nil {
		_ = os.RemoveAll(staging)
		return "", "", fmt.Errorf("probe staged Codex runtime: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		if !completeBundle(destination) {
			_ = os.RemoveAll(staging)
			return "", "", err
		}
		_ = os.RemoveAll(staging)
	}
	return version, filepath.Join(destination, executableName()), nil
}

func (m *Manager) probe(path string) error {
	if m.Probe == nil {
		return nil
	}
	return m.Probe(path)
}

func probeExecutable(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	configureHiddenProcess(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --version failed: %w", filepath.Base(path), err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if strings.TrimSpace(string(output)) == "" {
		return errors.New("Codex version probe returned no output")
	}
	return nil
}

func completeBundle(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	for _, name := range RequiredFiles() {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() || info.Size() == 0 {
			return false
		}
	}
	return true
}

func versionID(dir string) (string, error) {
	hash := sha256.New()
	for _, name := range RequiredFiles() {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s:%d:%d\n", name, info.Size(), info.ModTime().UTC().UnixNano())
	}
	slug := sanitizeVersion(filepath.Base(filepath.Clean(dir)))
	if slug == "" {
		slug = "codex"
	}
	return slug + "-" + hex.EncodeToString(hash.Sum(nil))[:12], nil
}

func sanitizeVersion(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out.WriteRune(r)
		}
	}
	if out.Len() > 48 {
		return out.String()[:48]
	}
	return out.String()
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (m *Manager) versionDir(version string) string {
	return filepath.Join(m.Root, "versions", version)
}

func (m *Manager) executablePath(version string) string {
	if version == "" {
		return ""
	}
	return filepath.Join(m.versionDir(version), executableName())
}

func (m *Manager) pruneUnused(active, previous string) {
	root := filepath.Join(m.Root, "versions")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	keep := map[string]struct{}{active: {}}
	if previous != "" {
		keep[previous] = struct{}{}
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasSuffix(entry.Name(), ".staging") {
			continue
		}
		if _, ok := keep[entry.Name()]; ok {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, entry.Name()))
	}
}

func (m *Manager) loadState() (state, error) {
	paths := []string{filepath.Join(m.Root, "state.json"), filepath.Join(m.Root, "state.json.old")}
	var lastErr error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		var value state
		if err := json.Unmarshal(data, &value); err != nil {
			lastErr = err
			continue
		}
		if value.Version != stateVersion || value.Active == "" {
			lastErr = errors.New("invalid Codex runtime state")
			continue
		}
		return value, nil
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return state{}, lastErr
}

func (m *Manager) writeState(value state) error {
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return err
	}
	value.Version = stateVersion
	value.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	current := filepath.Join(m.Root, "state.json")
	backup := filepath.Join(m.Root, "state.json.old")
	next := filepath.Join(m.Root, "state.json.new")
	if err := os.WriteFile(next, data, 0o600); err != nil {
		return err
	}
	if old, err := os.ReadFile(current); err == nil {
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return err
		}
	}
	if err := replaceFile(next, current); err != nil {
		return err
	}
	return nil
}

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}
