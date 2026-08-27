package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestHomeShowsCurrentSessionAndPersistentInboxCount(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 26, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	current := model.Thread{ID: "home-current", Title: "检查下载队列", Status: "active", UpdatedAt: now.Unix()}
	storeForegroundTestSnapshot(t, service, current, appserver.ThreadReadSnapshot{
		Thread: current, LatestTurnID: "home-turn", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-72 * time.Second).Format(time.RFC3339Nano),
	}, now)
	if err := service.store.SetState(ctx, foregroundThreadStateKey(123456789, 0), current.ID); err != nil {
		t.Fatalf("SetState foreground failed: %v", err)
	}
	if err := service.saveInboxItem(ctx, 123456789, 0, sessionInboxItem{ThreadID: "background-one", Title: "后台完成", State: notificationCompleted, UpdatedAt: now.Add(-time.Minute).Unix()}); err != nil {
		t.Fatalf("saveInboxItem(first) failed: %v", err)
	}
	if err := service.saveInboxItem(ctx, 123456789, 0, sessionInboxItem{ThreadID: "background-two", Title: "等待确认", State: notificationNeedsInput, UpdatedAt: now.Unix()}); err != nil {
		t.Fatalf("saveInboxItem(second) failed: %v", err)
	}

	response, err := service.handleCommand(ctx, 123456789, 0, "/home", 0)
	if err != nil {
		t.Fatalf("handleCommand(/home) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.Text, "检查下载队列") || !strings.Contains(response.Text, "处理中 · 1m 12s") || !strings.Contains(response.Text, "2 个后台会话需要关注") {
		t.Fatalf("home response = %#v", response)
	}
	for _, label := range []string{"查看当前进度", "切换会话", "新建会话", "待处理 2"} {
		if !strings.Contains(buttonTexts(response.Buttons), label) {
			t.Fatalf("home buttons = %#v, missing %q", response.Buttons, label)
		}
	}
}

func TestStartOpensTheSessionHome(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	response, err := service.handleCommand(context.Background(), 123456789, 0, "/start", 0)
	if err != nil {
		t.Fatalf("handleCommand(/start) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.Text, "Codex 首页") || !strings.Contains(buttonTexts(response.Buttons), "新建会话") {
		t.Fatalf("start response = %#v, want session home", response)
	}
}

func TestHomeNewSessionChooserArmsTitleThenPromptInPlace(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	home, err := service.handleCommand(ctx, 123456789, 0, "/home", 0)
	if err != nil {
		t.Fatalf("handleCommand(/home) failed: %v", err)
	}
	newMenuToken := callbackTokenForButton(home.Buttons, "新建会话")
	if newMenuToken == "" {
		t.Fatalf("home buttons = %#v, want new-session chooser", home.Buttons)
	}
	if _, err := service.HandleCallback(ctx, 123456789, 0, 77, 123456789, newMenuToken); err != nil {
		t.Fatalf("HandleCallback(home_new_menu) failed: %v", err)
	}
	if len(sender.edits) != 1 || sender.edits[0].messageID != 77 {
		t.Fatalf("chooser edits = %#v, want in-place edit", sender.edits)
	}
	newThreadToken := callbackTokenForButton(sender.edits[0].buttons, "新建普通会话")
	if newThreadToken == "" {
		t.Fatalf("chooser buttons = %#v", sender.edits[0].buttons)
	}
	if _, err := service.HandleCallback(ctx, 123456789, 0, 77, 123456789, newThreadToken); err != nil {
		t.Fatalf("HandleCallback(home_new_thread) failed: %v", err)
	}
	if len(sender.edits) != 2 || sender.edits[1].messageID != 77 || !strings.Contains(sender.edits[1].text, "请输入新会话的标题") {
		t.Fatalf("title edits = %#v, want in-place title prompt", sender.edits)
	}
	state, ok, expired, err := service.pendingNewThreadState(ctx, 123456789, 0)
	if err != nil || !ok || expired || state.Kind != pendingNewThreadKindWithoutCWD || state.Stage != pendingNewThreadStageTitle {
		t.Fatalf("pending state = %#v ok=%t expired=%t err=%v", state, ok, expired, err)
	}
}

func TestInboxPersistsBackgroundAttentionAndSwitchClearsIt(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 26, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	foreground := model.Thread{ID: "inbox-foreground", Title: "当前会话", Status: "active", UpdatedAt: now.Unix()}
	storeForegroundTestSnapshot(t, service, foreground, appserver.ThreadReadSnapshot{Thread: foreground, LatestTurnID: "foreground-turn", LatestTurnStatus: "inProgress", LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano)}, now)
	service.syncThreadPanelToTarget(ctx, target, foreground.ID, false, model.PanelSourceGlobalObserver)

	background := model.Thread{ID: "inbox-background", Title: "整理音乐目录", Status: "completed", UpdatedAt: now.Unix()}
	storeForegroundTestSnapshot(t, service, background, appserver.ThreadReadSnapshot{Thread: background, LatestTurnID: "background-turn", LatestTurnStatus: "completed", LatestFinalFP: "final-fp", LatestFinalText: "整理完成。"}, now)
	service.syncThreadPanelToTarget(ctx, target, background.ID, false, model.PanelSourceGlobalObserver)

	inbox, err := service.handleCommand(ctx, target.ChatID, target.TopicID, "/inbox", 0)
	if err != nil {
		t.Fatalf("handleCommand(/inbox) failed: %v", err)
	}
	if inbox == nil || !strings.Contains(inbox.Text, "待处理 · 1") || !strings.Contains(buttonTexts(inbox.Buttons), "整理音乐目录") || !strings.Contains(buttonTexts(inbox.Buttons), "已完成") {
		t.Fatalf("inbox = %#v", inbox)
	}

	service.live = &stubSession{threadReads: map[string]map[string]any{
		background.ID: {
			"thread": map[string]any{
				"id":     background.ID,
				"title":  background.Title,
				"status": map[string]any{"type": "completed"},
				"turns": []any{map[string]any{
					"id": "background-turn", "status": "completed",
					"items": []any{map[string]any{"type": "agentMessage", "phase": "final_answer", "text": "整理完成。"}},
				}},
			},
		},
	}}
	service.liveConnected = true
	switchToken := inbox.Buttons[0][0].CallbackData
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, 88, 123456789, switchToken); err != nil {
		t.Fatalf("HandleCallback(switch inbox item) failed: %v", err)
	}
	items, err := service.inboxItems(ctx, target.ChatID, target.TopicID)
	if err != nil || len(items) != 0 {
		t.Fatalf("inbox after switch = %#v err=%v, want empty", items, err)
	}

	reopened, err := service.store.ListState(ctx)
	if err != nil {
		t.Fatalf("ListState failed: %v", err)
	}
	for key := range reopened {
		if strings.HasPrefix(key, sessionInboxStatePrefix) {
			t.Fatalf("durable inbox state remained after switch: %q", key)
		}
	}
}

func TestPrimaryActivityCardUsesChineseStatusAndLabels(t *testing.T) {
	t.Parallel()

	message := renderNotificationCard(notificationCardView{
		Marker: "❤️", Title: "检查代码", State: notificationRunning,
		Summary: "正在处理请求", Activities: []activityCardItem{{Icon: "🔍", Text: "搜索实现", Current: true}}, Operations: 3,
	})
	for _, want := range []string{"<b>Codex</b> · <b>处理中</b>", "<b>进度</b>", "3 次操作"} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("card = %q, missing %q", message.Text, want)
		}
	}
	for _, unwanted := range []string{"Working", "Activity", "operations"} {
		if strings.Contains(message.Text, unwanted) {
			t.Fatalf("card = %q, contains untranslated %q", message.Text, unwanted)
		}
	}
}
