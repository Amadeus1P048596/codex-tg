package codexruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureBootstrapsPrivateRuntimeFromConfiguredBundle(t *testing.T) {
	root := t.TempDir()
	source := writeBundle(t, filepath.Join(root, "desktop", "release-a"), "a")
	manager := New(filepath.Join(root, "runtime"), []string{filepath.Join(root, "desktop")})
	manager.Probe = func(path string) error { return nil }

	result, err := manager.Ensure(filepath.Join(source, executableName()))
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}
	if result.Source != "bootstrap" {
		t.Fatalf("Source = %q, want bootstrap", result.Source)
	}
	if !result.NeedsValidation {
		t.Fatal("NeedsValidation = false, want first daemon start validated")
	}
	if filepath.Dir(result.Path) == source {
		t.Fatalf("Path = %q, want a private runtime copy", result.Path)
	}
	assertCompleteBundle(t, filepath.Dir(result.Path))
}

func TestConfirmMarksActiveRuntimeValidated(t *testing.T) {
	root := t.TempDir()
	source := writeBundle(t, filepath.Join(root, "desktop", "release-a"), "a")
	manager := New(filepath.Join(root, "runtime"), []string{filepath.Join(root, "desktop")})
	manager.Probe = func(path string) error { return nil }
	first, err := manager.Ensure(filepath.Join(source, executableName()))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Confirm(first.Version); err != nil {
		t.Fatalf("Confirm failed: %v", err)
	}
	second, err := manager.Ensure(filepath.Join(source, executableName()))
	if err != nil {
		t.Fatal(err)
	}
	if second.NeedsValidation {
		t.Fatal("NeedsValidation = true after Confirm")
	}
}

func TestEnsureUsesPrivateRuntimeWhenConfiguredBundleWasDeleted(t *testing.T) {
	root := t.TempDir()
	source := writeBundle(t, filepath.Join(root, "desktop", "release-a"), "a")
	manager := New(filepath.Join(root, "runtime"), []string{filepath.Join(root, "desktop")})
	manager.Probe = func(path string) error { return nil }
	first, err := manager.Ensure(filepath.Join(source, executableName()))
	if err != nil {
		t.Fatalf("first Ensure failed: %v", err)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatalf("RemoveAll source failed: %v", err)
	}

	second, err := manager.Ensure(filepath.Join(source, executableName()))
	if err != nil {
		t.Fatalf("second Ensure failed: %v", err)
	}
	if second.Path != first.Path {
		t.Fatalf("Path = %q, want retained private runtime %q", second.Path, first.Path)
	}
	if second.Source != "active" {
		t.Fatalf("Source = %q, want active", second.Source)
	}
}

func TestSyncPromotesNewestCompleteBundleAndKeepsPrevious(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "desktop")
	sourceA := writeBundle(t, filepath.Join(desktop, "release-a"), "a")
	manager := New(filepath.Join(root, "runtime"), []string{desktop})
	manager.Probe = func(path string) error { return nil }
	first, err := manager.Ensure(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	sourceB := writeBundle(t, filepath.Join(desktop, "release-b"), "b")
	result, err := manager.Sync(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if !result.Promoted {
		t.Fatal("Promoted = false, want true")
	}
	if result.Path == first.Path {
		t.Fatalf("Path = %q, want a new active runtime", result.Path)
	}
	if result.PreviousPath != first.Path {
		t.Fatalf("PreviousPath = %q, want %q", result.PreviousPath, first.Path)
	}
	if got, err := os.ReadFile(result.Path); err != nil || string(got) != "b-codex.exe" {
		t.Fatalf("active codex content = %q, %v; want release b", got, err)
	}
	if filepath.Dir(result.Path) == sourceB {
		t.Fatalf("Path = %q, want private copy", result.Path)
	}
}

func TestSyncIgnoresIncompleteNewerBundle(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "desktop")
	sourceA := writeBundle(t, filepath.Join(desktop, "release-a"), "a")
	manager := New(filepath.Join(root, "runtime"), []string{desktop})
	manager.Probe = func(path string) error { return nil }
	first, err := manager.Ensure(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	incomplete := filepath.Join(desktop, "release-b")
	if err := os.MkdirAll(incomplete, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incomplete, executableName()), []byte("b"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Sync(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if result.Promoted || result.Path != first.Path {
		t.Fatalf("result = %+v, want unchanged active runtime", result)
	}
}

func TestRollbackSwapsActiveAndPrevious(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "desktop")
	sourceA := writeBundle(t, filepath.Join(desktop, "release-a"), "a")
	manager := New(filepath.Join(root, "runtime"), []string{desktop})
	manager.Probe = func(path string) error { return nil }
	first, err := manager.Ensure(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = writeBundle(t, filepath.Join(desktop, "release-b"), "b")
	second, err := manager.Sync(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatal(err)
	}

	rolledBack, err := manager.Rollback()
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if rolledBack.Path != first.Path || rolledBack.PreviousPath != second.Path {
		t.Fatalf("Rollback = %+v, want active %q and previous %q", rolledBack, first.Path, second.Path)
	}
}

func TestSyncKeepsOnlyActiveAndPreviousVersions(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "desktop")
	manager := New(filepath.Join(root, "runtime"), []string{desktop})
	manager.Probe = func(path string) error { return nil }
	sourceA := writeBundle(t, filepath.Join(desktop, "release-a"), "a")
	first, err := manager.Ensure(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Confirm(first.Version); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = writeBundle(t, filepath.Join(desktop, "release-b"), "bb")
	second, err := manager.Sync(filepath.Join(sourceA, executableName()))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Confirm(second.Version); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = writeBundle(t, filepath.Join(desktop, "release-c"), "ccc")
	if _, err := manager.Sync(filepath.Join(sourceA, executableName())); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(manager.Root, "versions"))
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasSuffix(entry.Name(), ".staging") {
			versions = append(versions, entry.Name())
		}
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %v, want only active and previous", versions)
	}
}

func writeBundle(t *testing.T, dir, marker string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range RequiredFiles() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(marker+"-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	for _, name := range RequiredFiles() {
		if err := os.Chtimes(filepath.Join(dir, name), now, now); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func assertCompleteBundle(t *testing.T, dir string) {
	t.Helper()
	for _, name := range RequiredFiles() {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing copied runtime file %s: %v", name, err)
		}
	}
}
