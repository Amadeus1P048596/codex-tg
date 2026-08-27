package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

type recordedChatAction struct {
	chatID  int64
	topicID int64
	action  string
}

type typingRecordingSender struct {
	*recordingSender
	actions []recordedChatAction
	err     error
}

func (s *typingRecordingSender) SendChatAction(_ context.Context, chatID, topicID int64, action string) error {
	s.actions = append(s.actions, recordedChatAction{chatID: chatID, topicID: topicID, action: action})
	return s.err
}

func TestWorkingCardWaitsFourSecondsWhileSendingTyping(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	base := &recordingSender{}
	sender := &typingRecordingSender{recordingSender: base}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 25, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	thread := model.Thread{ID: "thread-delayed-card", Title: "Delayed card", ProjectName: "Codex", UpdatedAt: now.Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:              thread,
		LatestTurnID:        "turn-delayed-card",
		LatestTurnStatus:    "inProgress",
		LatestTurnStartedAt: now.Format(time.RFC3339Nano),
	}, now)
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)
	if len(base.messages) != 0 || len(sender.actions) != 1 || sender.actions[0].action != "typing" {
		t.Fatalf("messages=%#v actions=%#v, want typing only during grace period", base.messages, sender.actions)
	}

	now = now.Add(workingCardDelay + time.Millisecond)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	if len(base.messages) != 1 || !strings.Contains(base.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("messages=%#v, want one Working card after grace period", base.messages)
	}
}

func TestWorkingCardFallsBackToMessageWhenTypingFails(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	base := &recordingSender{}
	sender := &typingRecordingSender{recordingSender: base, err: errors.New("chat action unavailable")}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 25, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	thread := model.Thread{ID: "thread-typing-fallback", Title: "Typing fallback", ProjectName: "Codex", UpdatedAt: now.Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:              thread,
		LatestTurnID:        "turn-typing-fallback",
		LatestTurnStatus:    "inProgress",
		LatestTurnStartedAt: now.Format(time.RFC3339Nano),
	}, now)
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)
	if len(sender.actions) != 1 || len(base.messages) != 1 || !strings.Contains(base.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("messages=%#v actions=%#v, want Working card fallback after typing error", base.messages, sender.actions)
	}
}

func TestWorkingCardActivityEditsAreThrottled(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.August, 25, 11, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	thread := model.Thread{ID: "thread-throttled-card", Title: "Throttle", ProjectName: "Codex", UpdatedAt: now.Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}
	initial := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-throttled-card", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	if err := service.store.UpsertSnapshot(ctx, thread.ID, initial); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	withTool := appserver.CompactSnapshot(&initial, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-throttled-card", LatestTurnStatus: "inProgress",
		LatestToolID: "tool-test", LatestToolKind: "commandExecution", LatestToolLabel: "go test ./internal/daemon/...", LatestToolStatus: "running", LatestToolFP: "tool-test-running", LatestToolLiveCurrent: true,
	}, now.Add(time.Second))
	if err := service.store.UpsertSnapshot(ctx, thread.ID, withTool); err != nil {
		t.Fatalf("UpsertSnapshot(withTool) failed: %v", err)
	}
	now = now.Add(time.Second)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.edits) != 0 {
		t.Fatalf("edits=%#v, want burst update throttled", sender.edits)
	}

	now = now.Add(activityEditFloor)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.edits) != 1 || !strings.Contains(sender.edits[0].text, "● 🧪 运行测试") {
		t.Fatalf("edits=%#v, want one aggregated activity edit after throttle floor", sender.edits)
	}
}

func TestLongFinalCompletesWorkingCardThenSendsFullResult(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Now().UTC()
	thread := model.Thread{ID: "thread-long-final", Title: "Long final", ProjectName: "Codex", UpdatedAt: now.Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}
	working := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-long-final", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	if err := service.store.UpsertSnapshot(ctx, thread.ID, working); err != nil {
		t.Fatalf("UpsertSnapshot(working) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	workingID := sender.messages[0].messageID

	longFinal := "已完成主要修改。\n\n" + strings.Repeat("详细结果与验证记录。", 180)
	completed := appserver.CompactSnapshot(&working, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-long-final", LatestTurnStatus: "completed",
		LatestFinalFP: "long-final-fp", LatestFinalText: longFinal,
	}, now.Add(time.Minute))
	if err := service.store.UpsertSnapshot(ctx, thread.ID, completed); err != nil {
		t.Fatalf("UpsertSnapshot(completed) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)

	if len(sender.edits) != 1 || sender.edits[0].messageID != workingID || !strings.Contains(sender.edits[0].text, "<b>已完成</b>") {
		t.Fatalf("edits=%#v, want Working card completed in place", sender.edits)
	}
	if len(sender.messages) < 2 || !strings.Contains(sender.messages[1].text, "Codex · 结果") {
		t.Fatalf("messages=%#v, want separate full result after the original Working card", sender.messages)
	}
	if len(sender.deletes) != 0 {
		t.Fatalf("deletes=%#v, want continuous message lifecycle", sender.deletes)
	}
}

func TestShortFinalCompletesWorkingCardAndSendsAudibleNotice(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Now().UTC()
	thread := model.Thread{ID: "thread-short-final-notice", Title: "Short final", ProjectName: "Codex", UpdatedAt: now.Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}
	working := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-short-final-notice", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	if err := service.store.UpsertSnapshot(ctx, thread.ID, working); err != nil {
		t.Fatalf("UpsertSnapshot(working) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	workingID := sender.messages[0].messageID

	completed := appserver.CompactSnapshot(&working, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-short-final-notice", LatestTurnStatus: "completed",
		LatestFinalFP: "short-final-fp", LatestFinalText: "已完成。",
	}, now.Add(time.Minute))
	if err := service.store.UpsertSnapshot(ctx, thread.ID, completed); err != nil {
		t.Fatalf("UpsertSnapshot(completed) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)

	if len(sender.edits) != 1 || sender.edits[0].messageID != workingID || !strings.Contains(sender.edits[0].text, "<b>已完成</b>") {
		t.Fatalf("edits=%#v, want Working card completed in place", sender.edits)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("messages=%#v, want Working card plus one completion notice", sender.messages)
	}
	notice := sender.messages[1]
	if notice.options.Silent || !strings.Contains(notice.text, "<b>Short final</b> 已完成") {
		t.Fatalf("completion notice=%#v, want audible titled completion", notice)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	if len(sender.messages) != 2 {
		t.Fatalf("messages=%#v, want completion notice deduplicated", sender.messages)
	}
}
