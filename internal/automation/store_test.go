package automation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreCRUDPreservesUnknownFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Date(2026, time.August, 31, 12, 30, 0, 0, time.Local)
	store := NewStore(root, func() time.Time { return now })

	created, err := store.Apply(map[string]any{
		"mode":                 "create",
		"name":                 "Daily project check",
		"prompt":               "Review the project and report important changes.",
		"rrule":                "FREQ=DAILY;BYHOUR=9;BYMINUTE=30",
		"status":               "ACTIVE",
		"kind":                 "cron",
		"projectId":            nil,
		"model":                "gpt-test",
		"reasoningEffort":      "medium",
		"executionEnvironment": "local",
		"notificationPolicy":   "failed_runs_only",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("create returned an empty id")
	}
	path := filepath.Join(root, id, "automation.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	for _, want := range []string{
		`version = 1`,
		`kind = "cron"`,
		`status = "ACTIVE"`,
		`execution_environment = "local"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("automation.toml missing %q:\n%s", want, data)
		}
	}

	data = append(data, []byte("future_field = 'keep-me'\n")...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	now = now.Add(time.Hour)
	updated, err := store.Apply(map[string]any{
		"mode":   "update",
		"id":     id,
		"status": "PAUSED",
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if got := updated["name"]; got != "Daily project check" {
		t.Fatalf("name = %#v, want preserved value", got)
	}
	if got := updated["status"]; got != "PAUSED" {
		t.Fatalf("status = %#v, want PAUSED", got)
	}
	if got := updated["future_field"]; got != "keep-me" {
		t.Fatalf("future_field = %#v, want preserved value", got)
	}
	if updated["created_at"] == updated["updated_at"] {
		t.Fatal("updated_at was not refreshed")
	}

	listed, err := store.Apply(map[string]any{"mode": "list"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	items, ok := listed["automations"].([]map[string]any)
	if !ok || len(items) != 1 || items[0]["id"] != id {
		t.Fatalf("automations = %#v, want created task", listed["automations"])
	}

	viewed, err := store.Apply(map[string]any{"mode": "view", "id": id})
	if err != nil {
		t.Fatalf("view failed: %v", err)
	}
	if viewed["prompt"] != "Review the project and report important changes." {
		t.Fatalf("prompt = %#v, want original prompt", viewed["prompt"])
	}

	deleted, err := store.Apply(map[string]any{"mode": "delete", "id": id})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v, want true", deleted["deleted"])
	}
	if _, err := os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
		t.Fatalf("automation directory still exists or stat failed: %v", err)
	}
}

func TestStoreRejectsUnsafeOrUnsupportedAutomation(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir(), time.Now)
	for name, args := range map[string]map[string]any{
		"path traversal": {"mode": "view", "id": "../desktop"},
		"heartbeat": {
			"mode": "create", "name": "follow up", "prompt": "continue", "rrule": "FREQ=HOURLY;INTERVAL=1", "status": "ACTIVE", "kind": "heartbeat",
		},
		"minute cron": {
			"mode": "create", "name": "too frequent", "prompt": "check", "rrule": "FREQ=MINUTELY;INTERVAL=5", "status": "ACTIVE", "kind": "cron", "projectId": nil, "model": "gpt-test", "reasoningEffort": "medium",
		},
		"dtstart": {
			"mode": "create", "name": "anchored", "prompt": "check", "rrule": "DTSTART:20260831T090000;FREQ=DAILY", "status": "ACTIVE", "kind": "cron", "projectId": nil, "model": "gpt-test", "reasoningEffort": "medium",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Apply(args); err == nil {
				t.Fatalf("Apply(%s) succeeded, want validation error", name)
			}
		})
	}
}

func TestStoreRequiresNativeCronRuntimeFields(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir(), time.Now)
	base := map[string]any{
		"mode":      "create",
		"name":      "Daily check",
		"prompt":    "Check the project.",
		"rrule":     "FREQ=DAILY;BYHOUR=9;BYMINUTE=0",
		"status":    "ACTIVE",
		"kind":      "cron",
		"model":     "gpt-test",
		"projectId": nil,
	}
	if _, err := store.Apply(base); err == nil || !strings.Contains(err.Error(), "reasoningEffort is required") {
		t.Fatalf("Apply error = %v, want missing reasoningEffort", err)
	}
	delete(base, "projectId")
	base["reasoningEffort"] = "medium"
	if _, err := store.Apply(base); err == nil || !strings.Contains(err.Error(), "projectId is required") {
		t.Fatalf("Apply error = %v, want missing projectId", err)
	}
}

func TestStoreMarksTasksAsTelegramOwnedAndPersistsCWD(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir(), time.Now)
	created, err := store.Apply(map[string]any{
		"mode": "create", "name": "Repository check", "prompt": "Review the repository.",
		"rrule": "FREQ=DAILY;BYHOUR=9;BYMINUTE=30", "status": "ACTIVE", "kind": "cron",
		"projectId": nil, "cwd": `C:\work\project`, "model": "gpt-test", "reasoningEffort": "medium",
		"executionEnvironment": "local",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created["owner"] != "codex-tg" {
		t.Fatalf("owner = %#v, want codex-tg", created["owner"])
	}
	if created["cwd"] != `C:\work\project` {
		t.Fatalf("cwd = %#v, want task working directory", created["cwd"])
	}
}

func TestStoreHidesAndProtectsDesktopHeartbeatTasks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "desktop-heartbeat")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	path := filepath.Join(dir, automationFileName)
	original := "version = 1\nid = \"desktop-heartbeat\"\nkind = \"heartbeat\"\nname = \"Desktop follow-up\"\nprompt = \"Continue the Desktop thread.\"\nstatus = \"ACTIVE\"\nrrule = \"FREQ=DAILY;BYHOUR=9;BYMINUTE=0\"\ntarget_thread_id = \"thread-owned-by-desktop\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	store := NewStore(root, time.Now)
	listed, err := store.Apply(map[string]any{"mode": "list"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if items := listed["automations"].([]map[string]any); len(items) != 0 {
		t.Fatalf("heartbeat leaked into Telegram list: %#v", items)
	}
	for _, mode := range []string{"view", "update", "delete"} {
		args := map[string]any{"mode": mode, "id": "desktop-heartbeat"}
		if mode == "update" {
			args["status"] = "PAUSED"
		}
		if _, err := store.Apply(args); err == nil || !strings.Contains(err.Error(), "standalone cron") {
			t.Fatalf("%s error = %v, want standalone cron rejection", mode, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(after) != original {
		t.Fatal("heartbeat file changed after rejected Telegram operations")
	}
}
