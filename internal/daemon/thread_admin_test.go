package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestNewThreadUsesPromptTitleWhileAppServerTitleIsPlaceholder(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	threadID := "01a0399e-bec4-7a61-84cb-3b4029900ce1"
	prompt := "看下 zlibrary 下载队列现在还有多少书没下完"
	live := &stubSession{
		threadStartResult: map[string]any{"thread": map[string]any{"id": threadID, "title": threadID}},
		threadReads: map[string]map[string]any{
			threadID: {
				"thread": map[string]any{
					"id": threadID, "title": threadID, "status": map[string]any{"type": "active"},
					"turns": []any{map[string]any{"id": "started-turn", "status": "inProgress", "items": []any{}}},
				},
			},
		},
	}
	service.live = live
	service.liveConnected = true

	response, err := service.handleCommand(ctx, 123456789, 0, "/newthread "+prompt, 0)
	if err != nil {
		t.Fatalf("handleCommand(/newthread) failed: %v", err)
	}
	if response == nil || response.ThreadID != threadID {
		t.Fatalf("response = %#v, want created thread", response)
	}
	thread, err := service.store.GetThread(ctx, threadID)
	if err != nil || thread == nil || thread.Title != prompt {
		t.Fatalf("thread = %#v err=%v, want prompt-derived title %q", thread, err, prompt)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text, "<b>"+prompt+"</b>") || strings.Contains(sender.messages[0].text, "<b>"+threadID+"</b>") {
		t.Fatalf("messages = %#v, want prompt title and no UUID title", sender.messages)
	}
}

func TestNewThreadBecomesTheForegroundSession(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	oldThread := model.Thread{ID: "old-foreground", Title: "旧会话", Status: "idle", UpdatedAt: 10}
	if err := service.store.UpsertThread(ctx, oldThread); err != nil {
		t.Fatalf("UpsertThread(old) failed: %v", err)
	}
	if err := service.store.SetState(ctx, foregroundThreadStateKey(123456789, 0), oldThread.ID); err != nil {
		t.Fatalf("SetState foreground failed: %v", err)
	}
	newThreadID := "new-foreground"
	service.live = &stubSession{
		threadStartResult: map[string]any{"thread": map[string]any{"id": newThreadID, "title": newThreadID}},
		threadReads: map[string]map[string]any{
			newThreadID: {
				"thread": map[string]any{
					"id": newThreadID, "title": newThreadID, "status": map[string]any{"type": "active"},
					"turns": []any{map[string]any{"id": "new-turn", "status": "inProgress", "items": []any{}}},
				},
			},
		},
	}
	service.liveConnected = true

	response, err := service.handleCommand(ctx, 123456789, 0, "/newthread 新会话的首条消息", 0)
	if err != nil {
		t.Fatalf("handleCommand(/newthread) failed: %v", err)
	}
	if response == nil || response.ThreadID != newThreadID {
		t.Fatalf("response = %#v", response)
	}
	foreground, err := service.foregroundThreadID(ctx, 123456789, 0)
	if err != nil || foreground != newThreadID {
		t.Fatalf("foreground = %q err=%v, want new session", foreground, err)
	}
}

func TestSyncThreadsPreservesPromptTitleUntilRuntimePublishesRealTitle(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	threadID := "01a0399e-bec4-7a61-84cb-3b4029900ce1"
	local := model.Thread{ID: threadID, Title: "查看下载队列", LastPreview: "查看下载队列", UpdatedAt: 10}
	if err := service.store.UpsertThread(ctx, local); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	service.live = &stubSession{threadListResult: map[string]any{"data": []any{
		map[string]any{"id": threadID, "title": threadID, "updatedAt": int64(11)},
	}}}
	service.liveConnected = true

	service.syncThreads(ctx, 10)
	stored, err := service.store.GetThread(ctx, threadID)
	if err != nil || stored == nil || stored.Title != local.Title {
		t.Fatalf("stored thread = %#v err=%v, want preserved local title", stored, err)
	}

	service.live.(*stubSession).threadListResult = map[string]any{"data": []any{
		map[string]any{"id": threadID, "title": "下载队列进度", "updatedAt": int64(12)},
	}}
	service.syncThreads(ctx, 10)
	stored, err = service.store.GetThread(ctx, threadID)
	if err != nil || stored == nil || stored.Title != "下载队列进度" {
		t.Fatalf("stored thread = %#v err=%v, want runtime real title", stored, err)
	}
}

func TestTitleCommandRenamesCurrentThread(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	thread := model.Thread{ID: "title-current-thread", Title: "旧标题", Status: "idle", UpdatedAt: 10}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetBinding(ctx, 123456789, 0, thread.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}
	live := &stubSession{}
	service.live = live
	service.liveConnected = true

	response, err := service.handleCommand(ctx, 123456789, 0, "/title 新的会话标题", 0)
	if err != nil {
		t.Fatalf("handleCommand(/title) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.Text, "新的会话标题") {
		t.Fatalf("response = %#v, want renamed confirmation", response)
	}
	if len(live.threadSetNameCalls) != 1 || live.threadSetNameCalls[0].threadID != thread.ID || live.threadSetNameCalls[0].name != "新的会话标题" {
		t.Fatalf("ThreadSetName calls = %#v", live.threadSetNameCalls)
	}
	stored, err := service.store.GetThread(ctx, thread.ID)
	if err != nil || stored == nil || stored.Title != "新的会话标题" {
		t.Fatalf("stored thread = %#v err=%v", stored, err)
	}

	live.threadListResult = map[string]any{"data": []any{
		map[string]any{"id": thread.ID, "title": "运行时自动生成的新标题", "updatedAt": int64(12)},
	}}
	service.syncThreads(ctx, 10)
	stored, err = service.store.GetThread(ctx, thread.ID)
	if err != nil || stored == nil || stored.Title != "新的会话标题" {
		t.Fatalf("stored thread after runtime refresh = %#v err=%v, want user-owned title", stored, err)
	}
}

func TestTitleCommandEditsCurrentActivityCardInPlace(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	thread := model.Thread{ID: "title-card-thread", Title: "旧卡片标题", Status: "active", UpdatedAt: now.Unix()}
	storeForegroundTestSnapshot(t, service, thread, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "title-card-turn", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	if err := service.store.SetBinding(ctx, 123456789, 0, thread.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}
	service.live = &stubSession{}
	service.liveConnected = true
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text, "旧卡片标题") {
		t.Fatalf("initial card = %#v", sender.messages)
	}

	if _, err := service.handleCommand(ctx, 123456789, 0, "/title 新卡片标题", 0); err != nil {
		t.Fatalf("handleCommand(/title) failed: %v", err)
	}
	if len(sender.edits) != 1 || !strings.Contains(sender.edits[0].text, "<b>新卡片标题</b>") || strings.Contains(sender.edits[0].text, "旧卡片标题") {
		t.Fatalf("card edits = %#v, want in-place renamed title", sender.edits)
	}
}

func TestCurrentCommandShowsForegroundThreadTitleStatusAndShortID(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	thread := model.Thread{ID: "01a0399e-bec4-7a61-84cb-3b4029900ce1", Title: "查看下载队列", Status: "active", UpdatedAt: 10}
	storeForegroundTestSnapshot(t, service, thread, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "current-turn", LatestTurnStatus: "inProgress",
	}, time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC))
	if err := service.store.SetBinding(ctx, 123456789, 0, thread.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}
	if err := service.store.SetState(ctx, foregroundThreadStateKey(123456789, 0), thread.ID); err != nil {
		t.Fatalf("SetState foreground failed: %v", err)
	}

	response, err := service.handleCommand(ctx, 123456789, 0, "/current", 0)
	if err != nil {
		t.Fatalf("handleCommand(/current) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.Text, "当前会话") || !strings.Contains(response.Text, "查看下载队列") || !strings.Contains(response.Text, "处理中") || !strings.Contains(response.Text, "T:01a0399e") {
		t.Fatalf("response = %#v", response)
	}
	if strings.Contains(response.Text, thread.ID) {
		t.Fatalf("response leaked full UUID: %q", response.Text)
	}
}

func TestArchiveCommandRequiresConfirmationThenArchivesCurrentThread(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	thread := model.Thread{ID: "archive-current-thread", Title: "准备归档的会话", Status: "idle", UpdatedAt: 10}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetBinding(ctx, 123456789, 0, thread.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}
	if err := service.store.SetState(ctx, foregroundThreadStateKey(123456789, 0), thread.ID); err != nil {
		t.Fatalf("SetState foreground failed: %v", err)
	}
	live := &stubSession{}
	service.live = live
	service.liveConnected = true

	confirm, err := service.handleCommand(ctx, 123456789, 0, "/archive", 0)
	if err != nil {
		t.Fatalf("handleCommand(/archive) failed: %v", err)
	}
	if confirm == nil || !strings.Contains(confirm.Text, "确认归档") || !strings.Contains(confirm.Text, thread.Title) || len(confirm.Buttons) != 1 || len(confirm.Buttons[0]) != 2 {
		t.Fatalf("confirmation = %#v", confirm)
	}
	if len(live.threadArchiveCalls) != 0 {
		t.Fatalf("archive occurred before confirmation: %#v", live.threadArchiveCalls)
	}

	response, err := service.HandleCallback(ctx, 123456789, 0, 77, 123456789, confirm.Buttons[0][0].CallbackData)
	if err != nil {
		t.Fatalf("HandleCallback(archive_confirm) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.CallbackText, "已归档") {
		t.Fatalf("response = %#v", response)
	}
	if len(live.threadArchiveCalls) != 1 || live.threadArchiveCalls[0] != thread.ID {
		t.Fatalf("ThreadArchive calls = %#v", live.threadArchiveCalls)
	}
	stored, err := service.store.GetThread(ctx, thread.ID)
	if err != nil || stored == nil || !stored.Archived {
		t.Fatalf("stored thread = %#v err=%v, want archived", stored, err)
	}
	binding, err := service.store.GetBinding(ctx, 123456789, 0)
	if err != nil || binding != nil {
		t.Fatalf("binding = %#v err=%v, want cleared", binding, err)
	}
	foreground, err := service.foregroundThreadID(ctx, 123456789, 0)
	if err != nil || foreground != "" {
		t.Fatalf("foreground = %q err=%v, want cleared", foreground, err)
	}
	if len(sender.edits) != 1 || !strings.Contains(sender.edits[0].text, "已归档") || !strings.Contains(sender.edits[0].text, "当前没有活动会话") {
		t.Fatalf("edits = %#v, want archive landing", sender.edits)
	}
	if got := buttonTexts(sender.edits[0].buttons); !strings.Contains(got, "切换其他会话") || !strings.Contains(got, "新建会话") {
		t.Fatalf("archive landing buttons = %#v", sender.edits[0].buttons)
	}
}

func TestArchiveCommandBlocksRunningOrWaitingThread(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		snapshot appserver.ThreadReadSnapshot
	}{
		{name: "running", snapshot: appserver.ThreadReadSnapshot{LatestTurnID: "turn-running", LatestTurnStatus: "inProgress"}},
		{name: "waiting", snapshot: appserver.ThreadReadSnapshot{LatestTurnID: "turn-waiting", LatestTurnStatus: "inProgress", WaitingOnReply: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t)
			ctx := context.Background()
			thread := model.Thread{ID: "archive-" + test.name, Title: "不可归档会话", Status: "active", UpdatedAt: 10}
			test.snapshot.Thread = thread
			storeForegroundTestSnapshot(t, service, thread, test.snapshot, time.Now())
			if err := service.store.SetBinding(ctx, 123456789, 0, thread.ID, model.BindingModeBound); err != nil {
				t.Fatalf("SetBinding failed: %v", err)
			}
			live := &stubSession{}
			service.live = live
			service.liveConnected = true

			response, err := service.handleCommand(ctx, 123456789, 0, "/archive", 0)
			if err != nil {
				t.Fatalf("handleCommand(/archive) failed: %v", err)
			}
			if response == nil || !strings.Contains(response.Text, "暂时不能归档") || strings.Contains(response.Text, "确认归档当前会话") {
				t.Fatalf("response = %#v, want active-session guard", response)
			}
			if len(live.threadArchiveCalls) != 0 {
				t.Fatalf("ThreadArchive calls = %#v, want none", live.threadArchiveCalls)
			}
		})
	}
}

func TestArchiveConfirmationCanBeCancelledWithoutArchiving(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	thread := model.Thread{ID: "archive-cancel-thread", Title: "不归档", Status: "idle", UpdatedAt: 10}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetBinding(ctx, 123456789, 0, thread.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}
	service.live = &stubSession{}
	service.liveConnected = true

	confirm, err := service.handleCommand(ctx, 123456789, 0, "/archive", 0)
	if err != nil {
		t.Fatalf("handleCommand(/archive) failed: %v", err)
	}
	response, err := service.HandleCallback(ctx, 123456789, 0, 78, 123456789, confirm.Buttons[0][1].CallbackData)
	if err != nil {
		t.Fatalf("HandleCallback(archive_cancel) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.CallbackText, "已取消") {
		t.Fatalf("response = %#v", response)
	}
	live := service.live.(*stubSession)
	if len(live.threadArchiveCalls) != 0 {
		t.Fatalf("ThreadArchive calls = %#v, want none", live.threadArchiveCalls)
	}
	stored, _ := service.store.GetThread(ctx, thread.ID)
	if stored == nil || stored.Archived {
		t.Fatalf("stored thread = %#v, want unarchived", stored)
	}
	if len(sender.edits) != 1 || !strings.Contains(sender.edits[0].text, "已取消归档") {
		t.Fatalf("edits = %#v", sender.edits)
	}
}

func TestUnarchiveListsTenPerPageAndRestoresClickedThread(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	firstPage := make([]any, 0, 10)
	for index := 1; index <= 10; index++ {
		firstPage = append(firstPage, map[string]any{"id": fmt.Sprintf("archived-%02d", index), "title": fmt.Sprintf("归档会话 %02d", index), "updatedAt": int64(100 - index)})
	}
	secondPage := []any{
		map[string]any{"id": "archived-11", "title": "归档会话 11", "updatedAt": int64(89)},
		map[string]any{"id": "archived-12", "title": "归档会话 12", "updatedAt": int64(88)},
	}
	live := &stubSession{archivedThreadPages: map[string]map[string]any{
		"":         {"data": firstPage, "nextCursor": "cursor-2"},
		"cursor-2": {"data": secondPage},
	}}
	service.live = live
	service.liveConnected = true

	pageOne, err := service.handleCommand(ctx, 123456789, 0, "/unarchive", 0)
	if err != nil {
		t.Fatalf("handleCommand(/unarchive) failed: %v", err)
	}
	if pageOne == nil || !strings.Contains(pageOne.Text, "第 1 页") || len(pageOne.Buttons) != 11 {
		t.Fatalf("page one = %#v, want ten title rows plus navigation", pageOne)
	}
	if pageOne.Buttons[0][0].Text != "归档会话 01" || pageOne.Buttons[10][0].Text != "下一页 ›" {
		t.Fatalf("page one buttons = %#v", pageOne.Buttons)
	}

	next, err := service.HandleCallback(ctx, 123456789, 0, 90, 123456789, pageOne.Buttons[10][0].CallbackData)
	if err != nil {
		t.Fatalf("HandleCallback(unarchive_page) failed: %v", err)
	}
	if next == nil || !strings.Contains(next.CallbackText, "第 2 页") || len(sender.edits) != 1 {
		t.Fatalf("next response = %#v edits=%#v", next, sender.edits)
	}
	pageTwoEdit := sender.edits[0]
	if !strings.Contains(pageTwoEdit.text, "第 2 页") || len(pageTwoEdit.buttons) != 3 || pageTwoEdit.buttons[0][0].Text != "归档会话 11" || pageTwoEdit.buttons[2][0].Text != "‹ 上一页" {
		t.Fatalf("page two edit = %#v", pageTwoEdit)
	}
	if got := strings.Join(live.archivedThreadListCursors, ","); got != ",cursor-2" {
		t.Fatalf("archived list cursors = %#v", live.archivedThreadListCursors)
	}

	restored, err := service.HandleCallback(ctx, 123456789, 0, 90, 123456789, pageTwoEdit.buttons[0][0].CallbackData)
	if err != nil {
		t.Fatalf("HandleCallback(unarchive_thread) failed: %v", err)
	}
	if restored == nil || !strings.Contains(restored.CallbackText, "已恢复") {
		t.Fatalf("restored response = %#v", restored)
	}
	if len(live.threadUnarchiveCalls) != 1 || live.threadUnarchiveCalls[0] != "archived-11" {
		t.Fatalf("ThreadUnarchive calls = %#v", live.threadUnarchiveCalls)
	}
	stored, err := service.store.GetThread(ctx, "archived-11")
	if err != nil || stored == nil || stored.Archived {
		t.Fatalf("stored restored thread = %#v err=%v", stored, err)
	}
	if len(sender.edits) != 2 || !strings.Contains(sender.edits[1].text, "已恢复") || !strings.Contains(sender.edits[1].text, "归档会话 11") {
		t.Fatalf("restore edits = %#v", sender.edits)
	}
	if got := buttonTexts(sender.edits[1].buttons); !strings.Contains(got, "切换至该会话") || !strings.Contains(got, "继续查看归档") {
		t.Fatalf("restore buttons = %#v", sender.edits[1].buttons)
	}
}

func buttonTexts(rows [][]model.ButtonSpec) string {
	var values []string
	for _, row := range rows {
		for _, button := range row {
			values = append(values, button.Text)
		}
	}
	return strings.Join(values, "\n")
}

func TestTitleAndArchiveCommandsRequireCurrentThread(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	service.live = &stubSession{}
	service.liveConnected = true

	for _, command := range []string{"/title 新标题", "/archive"} {
		response, err := service.handleCommand(ctx, 123456789, 0, command, 0)
		if err != nil {
			t.Fatalf("handleCommand(%s) failed: %v", command, err)
		}
		if response == nil || !strings.Contains(response.Text, "当前没有已选择的会话") {
			t.Fatalf("handleCommand(%s) = %#v", command, response)
		}
	}
}

func TestArchiveConfirmationExpiresWhenCurrentThreadChanges(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	first := model.Thread{ID: "archive-first", Title: "第一个会话", UpdatedAt: time.Now().Unix()}
	second := model.Thread{ID: "archive-second", Title: "第二个会话", UpdatedAt: time.Now().Unix()}
	for _, thread := range []model.Thread{first, second} {
		if err := service.store.UpsertThread(ctx, thread); err != nil {
			t.Fatalf("UpsertThread failed: %v", err)
		}
	}
	if err := service.store.SetBinding(ctx, 123456789, 0, first.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding(first) failed: %v", err)
	}
	live := &stubSession{}
	service.live = live
	service.liveConnected = true
	confirm, err := service.handleCommand(ctx, 123456789, 0, "/archive", 0)
	if err != nil {
		t.Fatalf("handleCommand(/archive) failed: %v", err)
	}
	if err := service.store.SetBinding(ctx, 123456789, 0, second.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding(second) failed: %v", err)
	}
	response, err := service.HandleCallback(ctx, 123456789, 0, 91, 123456789, confirm.Buttons[0][0].CallbackData)
	if err != nil {
		t.Fatalf("HandleCallback(stale archive) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.CallbackText, "会话已切换") {
		t.Fatalf("response = %#v", response)
	}
	if len(live.threadArchiveCalls) != 0 {
		t.Fatalf("stale confirmation archived a thread: %#v", live.threadArchiveCalls)
	}
}
