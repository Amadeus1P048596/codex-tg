package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/automation"
	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestCompletedAutomationMCPEventAttachesSourceConversation(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	service.cfg.AutomationsDir = filepath.Join(service.cfg.Paths.Home, "automations")
	store := automation.NewStore(service.cfg.AutomationsDir, time.Now)
	created, err := store.Apply(map[string]any{
		"mode": "create", "name": "Daily review", "prompt": "Review and report.",
		"rrule": "FREQ=DAILY;BYHOUR=9;BYMINUTE=0", "status": "ACTIVE", "kind": "cron",
		"projectId": nil, "model": "gpt-test", "reasoningEffort": "medium", "executionEnvironment": "local",
	})
	if err != nil {
		t.Fatalf("create automation failed: %v", err)
	}
	taskID, _ := created["id"].(string)
	thread := model.Thread{ID: "source-thread", Title: "示例计划"}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	resultText, _ := json.Marshal(created)
	event := appserver.Event{Channel: "notification", Method: "item/completed", Params: map[string]any{
		"threadId": thread.ID,
		"item": map[string]any{
			"id": "mcp-call", "type": "mcpToolCall", "server": "codex_tg_automations",
			"tool": "automation_update", "status": "completed",
			"arguments": map[string]any{"mode": "create", "name": "Daily review"},
			"result":    map[string]any{"content": []any{map[string]any{"type": "text", "text": string(resultText)}}},
		},
	}}

	service.captureAutomationOriginFromEvent(ctx, event, thread)

	tasks, err := store.ListTasks()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks = %#v err=%v", tasks, err)
	}
	if tasks[0].ID != taskID || tasks[0].SourceThreadID != thread.ID || tasks[0].SourceThreadTitle != thread.Title {
		t.Fatalf("task origin = %#v, want source thread/title", tasks[0])
	}
}

func TestScheduledTerminalUsesPushServiceWithoutConversationPanel(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	service.cfg.AutomationsDir = filepath.Join(service.cfg.Paths.Home, "automations")
	store := automation.NewStore(service.cfg.AutomationsDir, time.Now)
	created, err := store.Apply(map[string]any{
		"mode": "create", "name": "训练提醒", "prompt": "生成今天的训练提示。",
		"rrule": "FREQ=DAILY;BYHOUR=9;BYMINUTE=0", "status": "ACTIVE", "kind": "cron",
		"projectId": nil, "model": "gpt-test", "reasoningEffort": "medium", "executionEnvironment": "local",
	})
	if err != nil {
		t.Fatalf("create automation failed: %v", err)
	}
	taskID, _ := created["id"].(string)
	if err := store.SetOriginIfMissing(taskID, "source-thread", "示例计划"); err != nil {
		t.Fatalf("SetOriginIfMissing failed: %v", err)
	}
	thread := model.Thread{ID: "scheduled-thread", Title: "训练提醒", Status: "completed", UpdatedAt: time.Now().UTC().Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetState(ctx, automationThreadStateKey(thread.ID), taskID); err != nil {
		t.Fatalf("SetState(thread task) failed: %v", err)
	}
	if err := service.saveAutomationRunState(ctx, taskID, automationRunState{SlotUnixMilli: 123, Status: "running", ThreadID: thread.ID, TurnID: "scheduled-turn", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("saveAutomationRunState failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "scheduled-turn", LatestTurnStatus: "completed",
		LatestFinalText: "今天进行 30 分钟轻松跑。", LatestFinalFP: "scheduled-final-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	sender := &recordingSender{}
	service.SetSender(sender)
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 {
		t.Fatalf("messages = %#v, want one de-duplicated push", sender.messages)
	}
	got := sender.messages[0].text + "\n" + sender.messages[0].plainText
	for _, want := range []string{"训练提醒", "示例计划", "30 分钟轻松跑"} {
		if !strings.Contains(got, want) {
			t.Fatalf("push = %q, want %q", got, want)
		}
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil || panel != nil {
		t.Fatalf("panel = %#v err=%v, scheduled run must not create a conversation card", panel, err)
	}
}

func TestScheduledFailedRunsOnlySuppressesSuccessfulPush(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	service.cfg.AutomationsDir = filepath.Join(service.cfg.Paths.Home, "automations")
	store := automation.NewStore(service.cfg.AutomationsDir, time.Now)
	created, err := store.Apply(map[string]any{
		"mode": "create", "name": "Silent success", "prompt": "Check.",
		"rrule": "FREQ=DAILY;BYHOUR=9;BYMINUTE=0", "status": "ACTIVE", "kind": "cron",
		"projectId": nil, "model": "gpt-test", "reasoningEffort": "medium", "executionEnvironment": "local",
		"notificationPolicy": "failed_runs_only",
	})
	if err != nil {
		t.Fatalf("create automation failed: %v", err)
	}
	taskID, _ := created["id"].(string)
	thread := model.Thread{ID: "scheduled-silent", Title: "Silent success", Status: "completed"}
	_ = service.store.UpsertThread(ctx, thread)
	_ = service.store.SetState(ctx, automationThreadStateKey(thread.ID), taskID)
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-silent", LatestTurnStatus: "completed", LatestFinalText: "No changes.", LatestFinalFP: "silent-final",
	}, time.Now().UTC())
	_ = service.store.UpsertSnapshot(ctx, thread.ID, snapshot)
	sender := &recordingSender{}
	service.SetSender(sender)

	service.syncThreadPanelToTarget(ctx, model.ObserverTarget{ChatID: 123456789, Enabled: true}, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 0 {
		t.Fatalf("messages = %#v, want successful push suppressed", sender.messages)
	}
}

func TestScheduledExecutionThreadIsTrackedButHiddenFromConversationSurfaces(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	thread := model.Thread{
		ID: "scheduled-internal", Title: "Internal schedule run", CWD: `C:\work\project`,
		ProjectName: "project", Status: "inProgress", ActiveTurnID: "scheduled-turn", UpdatedAt: time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetState(ctx, automationThreadStateKey(thread.ID), "task-one"); err != nil {
		t.Fatalf("SetState(thread task) failed: %v", err)
	}
	service.live = &stubSession{threadListResult: map[string]any{"data": []any{
		map[string]any{"id": thread.ID, "title": thread.Title, "cwd": thread.CWD, "status": thread.Status, "updatedAt": thread.UpdatedAt},
	}}}
	service.liveConnected = true

	if sessions, total, err := service.backgroundRunningSessions(ctx, "", 5); err != nil || len(sessions) != 0 || total != 0 {
		t.Fatalf("background sessions = %#v total=%d err=%v, want hidden", sessions, total, err)
	}
	if catalog, err := service.projectCatalog(ctx); err != nil || len(catalog.Workspaces) != 0 || len(catalog.Chats) != 0 {
		t.Fatalf("project catalog = %#v err=%v, want hidden", catalog, err)
	}
	if threads, ready, err := service.runtimeThreads(ctx, 8, ""); err != nil || !ready || len(threads) != 0 {
		t.Fatalf("runtime threads = %#v ready=%v err=%v, want hidden", threads, ready, err)
	}
	tracked := service.trackedThreads(ctx, 8)
	if len(tracked) != 1 || tracked[0].ID != thread.ID {
		t.Fatalf("tracked threads = %#v, internal scheduler thread must still be polled", tracked)
	}
}

func TestPushHTTPHandlerRequiresBearerAndDeduplicates(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	service.cfg.PushToken = strings.Repeat("x", 32)
	sender := &recordingSender{}
	service.SetSender(sender)
	handler := service.pushHTTPHandler()
	body := []byte(`{"source":"bank-gold","title":"银行金价提醒","body":"招商银行赎回价触发阈值。","idempotency_key":"gold-20260831-1","severity":"normal"}`)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/push", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+service.cfg.PushToken)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusAccepted {
			t.Fatalf("attempt %d status = %d body=%q, want accepted", attempt, response.Code, response.Body.String())
		}
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text+sender.messages[0].plainText, "银行金价提醒") {
		t.Fatalf("messages = %#v, want one generic bank-gold push", sender.messages)
	}
}

func TestPushHTTPHandlerRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	service.cfg.PushToken = strings.Repeat("x", 32)
	req := httptest.NewRequest(http.MethodPost, "/v1/push", strings.NewReader(strings.Repeat("x", pushHTTPMaxBodyBytes+1)))
	req.Header.Set("Authorization", "Bearer "+service.cfg.PushToken)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	service.pushHTTPHandler().ServeHTTP(response, req)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestNewRejectsUnsafePushListenerConfiguration(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	base := config.Config{Paths: config.Paths{
		Home: root, DataDir: filepath.Join(root, "data"), LogDir: filepath.Join(root, "logs"), DBPath: filepath.Join(root, "data", "state.sqlite"),
	}}
	for _, test := range []struct {
		name   string
		listen string
		token  string
	}{
		{name: "short token", listen: "127.0.0.1:8787", token: "short"},
		{name: "wildcard", listen: "0.0.0.0:8787", token: strings.Repeat("x", 32)},
		{name: "star wildcard", listen: "*:8787", token: strings.Repeat("x", 32)},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.PushListen = test.listen
			cfg.PushToken = test.token
			if service, err := New(cfg); err == nil {
				_ = service.Close()
				t.Fatalf("New accepted unsafe push config %q", test.listen)
			}
		})
	}
}

func TestStartFailsClosedWhenPushAddressIsAlreadyInUse(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	defer listener.Close()
	root := t.TempDir()
	service, err := New(config.Config{
		Paths:      config.Paths{Home: root, DataDir: filepath.Join(root, "data"), LogDir: filepath.Join(root, "logs"), DBPath: filepath.Join(root, "data", "state.sqlite")},
		PushListen: listener.Addr().String(), PushToken: strings.Repeat("x", 32),
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer service.Close()
	if err := service.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded with an occupied push address")
	}
	if phase, _ := service.store.GetState(context.Background(), "daemon.phase"); phase != "failed" {
		t.Fatalf("daemon.phase = %q, want failed", phase)
	}
	if ready, _ := service.store.GetState(context.Background(), "daemon.ready"); ready != "false" {
		t.Fatalf("daemon.ready = %q, want false", ready)
	}
}
