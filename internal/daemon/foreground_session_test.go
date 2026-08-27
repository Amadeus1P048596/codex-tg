package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestBackgroundThreadSuppressesProgressAndSendsOneCompletionNotice(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 25, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	foreground := model.Thread{ID: "foreground-thread", Title: "当前会话", Status: "active", UpdatedAt: now.Unix()}
	storeForegroundTestSnapshot(t, service, foreground, appserver.ThreadReadSnapshot{
		Thread: foreground, LatestTurnID: "foreground-turn", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	service.syncThreadPanelToTarget(ctx, target, foreground.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("foreground messages = %#v, want one Working card", sender.messages)
	}

	background := model.Thread{ID: "background-thread", Title: "后台会话", Status: "active", UpdatedAt: now.Unix()}
	working := appserver.ThreadReadSnapshot{
		Thread: background, LatestTurnID: "background-turn", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-8 * time.Second).Format(time.RFC3339Nano),
	}
	storeForegroundTestSnapshot(t, service, background, working, now)
	service.syncThreadPanelToTarget(ctx, target, background.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 1 {
		t.Fatalf("background progress created Telegram messages: %#v", sender.messages)
	}

	completed := working
	completed.LatestTurnStatus = "completed"
	completed.LatestFinalFP = "background-final-fp"
	completed.LatestFinalText = "后台任务已完成。"
	storeForegroundTestSnapshot(t, service, background, completed, now.Add(time.Minute))
	service.syncThreadPanelToTarget(ctx, target, background.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 2 {
		t.Fatalf("messages = %#v, want one compact background completion", sender.messages)
	}
	notice := sender.messages[1]
	if !strings.Contains(notice.text, "<b>后台会话</b> 已完成") || len(notice.buttons) != 1 || len(notice.buttons[0]) != 1 || notice.buttons[0][0].Text != "切换至该会话" {
		t.Fatalf("background notice = %#v, want titled completion and switch button", notice)
	}

	service.syncThreadPanelToTarget(ctx, target, background.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 2 {
		t.Fatalf("duplicate background completion was sent: %#v", sender.messages)
	}
}

func TestBackgroundThreadSendsOneNeedsInputNoticeWithoutShowingProgressCard(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 25, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	foreground := model.Thread{ID: "foreground-needs-input", Title: "当前会话", Status: "active", UpdatedAt: now.Unix()}
	storeForegroundTestSnapshot(t, service, foreground, appserver.ThreadReadSnapshot{
		Thread: foreground, LatestTurnID: "foreground-turn", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	service.syncThreadPanelToTarget(ctx, target, foreground.ID, false, model.PanelSourceGlobalObserver)

	background := model.Thread{ID: "background-needs-input", Title: "需要确认的会话", Status: "active", UpdatedAt: now.Unix()}
	waiting := appserver.ThreadReadSnapshot{
		Thread: background, LatestTurnID: "background-turn", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-8 * time.Second).Format(time.RFC3339Nano),
		WaitingOnReply:      true,
		PlanPrompt: &model.PlanPrompt{
			PromptID: "prompt-1", ThreadID: background.ID, TurnID: "background-turn",
			Question: "是否继续？", Fingerprint: "prompt-fp", Status: "pending",
		},
	}
	storeForegroundTestSnapshot(t, service, background, waiting, now)
	service.syncThreadPanelToTarget(ctx, target, background.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 2 {
		t.Fatalf("messages = %#v, want foreground card plus one compact needs-input notice", sender.messages)
	}
	notice := sender.messages[1]
	if !strings.Contains(notice.text, "<b>需要确认的会话</b> 需要输入") || strings.Contains(notice.text, "<b>处理中</b>") {
		t.Fatalf("background notice = %#v, want compact needs-input notice without progress card", notice)
	}
	if len(notice.buttons) != 1 || len(notice.buttons[0]) != 1 || notice.buttons[0][0].Text != "切换至该会话" {
		t.Fatalf("background notice buttons = %#v, want one switch button", notice.buttons)
	}

	service.syncThreadPanelToTarget(ctx, target, background.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 2 {
		t.Fatalf("duplicate needs-input notice was sent: %#v", sender.messages)
	}
}

func TestSwitchThreadHidesPreviousWorkingCardAndShowsSelectedCard(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 25, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	foreground := model.Thread{ID: "foreground-switch", Title: "原会话", Status: "active", UpdatedAt: now.Unix()}
	storeForegroundTestSnapshot(t, service, foreground, appserver.ThreadReadSnapshot{
		Thread: foreground, LatestTurnID: "foreground-turn", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	service.syncThreadPanelToTarget(ctx, target, foreground.ID, false, model.PanelSourceGlobalObserver)
	workingMessageID := sender.messages[0].messageID

	selected := model.Thread{ID: "selected-switch", Title: "已完成会话", Status: "completed", UpdatedAt: now.Add(time.Minute).Unix()}
	storeForegroundTestSnapshot(t, service, selected, appserver.ThreadReadSnapshot{
		Thread: selected, LatestTurnID: "selected-turn", LatestTurnStatus: "completed",
		LatestTurnStartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		LatestFinalFP:       "selected-final-fp", LatestFinalText: "完成结果。",
	}, now.Add(time.Minute))
	service.syncThreadPanelToTarget(ctx, target, selected.ID, false, model.PanelSourceGlobalObserver)
	notice := sender.messages[1]
	token := notice.buttons[0][0].CallbackData

	response, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, notice.messageID, 123456789, token)
	if err != nil {
		t.Fatalf("HandleCallback(switch_thread) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.CallbackText, "已切换") {
		t.Fatalf("response = %#v, want switch confirmation", response)
	}
	active, err := service.foregroundThreadID(ctx, target.ChatID, target.TopicID)
	if err != nil || active != selected.ID {
		t.Fatalf("foreground thread = %q err=%v, want %q", active, err, selected.ID)
	}
	if len(sender.deletes) != 2 || sender.deletes[0].messageID != workingMessageID || sender.deletes[1].messageID != notice.messageID {
		t.Fatalf("deletes = %#v, want previous Working card and clicked notice removed", sender.deletes)
	}
	if len(sender.messages) < 3 || !strings.Contains(sender.messages[2].text, "<b>已完成会话</b>") || !strings.Contains(sender.messages[2].text, "<b>已完成</b>") {
		t.Fatalf("messages = %#v, want selected completed card", sender.messages)
	}
	binding, err := service.store.GetBinding(ctx, target.ChatID, target.TopicID)
	if err != nil || binding == nil || binding.ThreadID != selected.ID {
		t.Fatalf("binding = %#v err=%v, want selected thread", binding, err)
	}
}

func storeForegroundTestSnapshot(t *testing.T, service *Service, thread model.Thread, snapshot appserver.ThreadReadSnapshot, now time.Time) {
	t.Helper()
	if err := service.store.UpsertThread(context.Background(), thread); err != nil {
		t.Fatalf("UpsertThread(%s) failed: %v", thread.ID, err)
	}
	compact := appserver.CompactSnapshot(nil, snapshot, now)
	if err := service.store.UpsertSnapshot(context.Background(), thread.ID, compact); err != nil {
		t.Fatalf("UpsertSnapshot(%s) failed: %v", thread.ID, err)
	}
}
