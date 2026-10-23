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

func TestPerRunNewTurnCreatesNewCardAfterTypingDelayWhenOldPanelStillActive(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	base := &recordingSender{}
	sender := &typingRecordingSender{recordingSender: base}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Date(2026, time.October, 17, 13, 55, 11, 0, time.UTC)
	service.now = func() time.Time { return now }

	thread := model.Thread{ID: "thread-new-turn-after-stale-active", Title: "Book search", ProjectName: "Codex", UpdatedAt: now.Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	oldPanel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       model.PanelSourceGlobalObserver,
		SummaryMessageID: 101,
		CurrentTurnID:    "turn-old",
		Status:           "inProgress",
		ArchiveEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:              thread,
		LatestTurnID:        "turn-new",
		LatestTurnStatus:    "inProgress",
		LatestTurnStartedAt: now.Format(time.RFC3339Nano),
	}, now)
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot(new turn) failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)
	if len(base.messages) != 0 || len(sender.actions) != 1 {
		t.Fatalf("messages=%#v actions=%#v, want typing-only grace period", base.messages, sender.actions)
	}

	now = now.Add(workingCardDelay + time.Millisecond)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	if len(base.messages) != 1 || len(base.edits) != 0 {
		t.Fatalf("messages=%#v edits=%#v, want a new Working card instead of editing stale message 101", base.messages, base.edits)
	}
	newMessageID := base.messages[0].messageID
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.ID == oldPanel.ID || panel.CurrentTurnID != "turn-new" || panel.SummaryMessageID != newMessageID {
		t.Fatalf("panel=%#v, want a new current panel for turn-new and message %d", panel, newMessageID)
	}

	completed := appserver.CompactSnapshot(&snapshot, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-new", LatestTurnStatus: "completed",
		LatestFinalFP: "turn-new-final", LatestFinalText: "Found the requested book.",
	}, now.Add(time.Second))
	if err := service.store.UpsertSnapshot(ctx, thread.ID, completed); err != nil {
		t.Fatalf("UpsertSnapshot(completed) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	if len(base.edits) != 0 || len(base.messages) != 2 || !hasHeaderKind(base.messages[1].text, "Final") {
		t.Fatalf("messages=%#v edits=%#v, want a new final result card", base.messages, base.edits)
	}
	if len(base.deletes) != 1 || base.deletes[0].messageID != newMessageID || base.deletes[0].messageID == 101 {
		t.Fatalf("deletes=%#v, want only the new turn's Working card %d removed", base.deletes, newMessageID)
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

func TestLongFinalReplacesWorkingCardThenSendsFullResult(t *testing.T) {
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

	if len(sender.edits) != 0 {
		t.Fatalf("edits=%#v, want terminal result sent as a new message", sender.edits)
	}
	if len(sender.messages) < 3 || !hasHeaderKind(sender.messages[1].text, "Final") || !strings.Contains(sender.messages[2].text, "Codex · 结果") {
		t.Fatalf("messages=%#v, want a new Final card followed by the full result", sender.messages)
	}
	if sender.messages[1].options.Silent {
		t.Fatalf("final options=%#v, want a notifying Final card", sender.messages[1].options)
	}
	if !sender.messages[2].options.Silent {
		t.Fatalf("full result options=%#v, want one completion notification only", sender.messages[2].options)
	}
	if len(sender.deletes) != 1 || sender.deletes[0].messageID != workingID {
		t.Fatalf("deletes=%#v, want old Working card %d removed", sender.deletes, workingID)
	}
}

func TestShortFinalReplacesWorkingCardWithNotifyingResult(t *testing.T) {
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

	if len(sender.edits) != 0 {
		t.Fatalf("edits=%#v, want terminal result sent as a new message", sender.edits)
	}
	if len(sender.messages) != 2 || !hasHeaderKind(sender.messages[1].text, "Final") || sender.messages[1].options.Silent {
		t.Fatalf("messages=%#v, want one new notifying Final card", sender.messages)
	}
	if len(sender.deletes) != 1 || sender.deletes[0].messageID != workingID {
		t.Fatalf("deletes=%#v, want old Working card %d removed", sender.deletes, workingID)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	if len(sender.messages) != 2 || len(sender.deletes) != 1 {
		t.Fatalf("messages=%#v, want repeated sync to stay silent", sender.messages)
	}
}

func TestFinalSendFailureKeepsWorkingCardAndRetries(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Now().UTC()
	thread := model.Thread{ID: "thread-final-send-retry", Title: "Final retry", ProjectName: "Codex", UpdatedAt: now.Unix()}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, Enabled: true}
	working := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-final-send-retry", LatestTurnStatus: "inProgress",
		LatestTurnStartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}, now)
	if err := service.store.UpsertSnapshot(ctx, thread.ID, working); err != nil {
		t.Fatalf("UpsertSnapshot(working) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	workingID := sender.messages[0].messageID

	completed := appserver.CompactSnapshot(&working, appserver.ThreadReadSnapshot{
		Thread: thread, LatestTurnID: "turn-final-send-retry", LatestTurnStatus: "completed",
		LatestFinalFP: "final-send-retry-fp", LatestFinalText: "已完成。",
	}, now.Add(time.Minute))
	if err := service.store.UpsertSnapshot(ctx, thread.ID, completed); err != nil {
		t.Fatalf("UpsertSnapshot(completed) failed: %v", err)
	}
	sender.sendErr = errors.New("forced final send failure")
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)

	if len(sender.deletes) != 0 {
		t.Fatalf("deletes=%#v, want Working card preserved after final send failure", sender.deletes)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID != workingID || panel.LastFinalNoticeFP != "" {
		t.Fatalf("panel=%#v, want retryable Working state on message %d", panel, workingID)
	}

	sender.sendErr = nil
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceTelegramInput)
	if len(sender.deletes) != 1 || sender.deletes[0].messageID != workingID {
		t.Fatalf("deletes=%#v, want Working card removed after successful retry", sender.deletes)
	}
	panel, err = service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel after retry failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID == workingID || panel.LastFinalNoticeFP != "final-send-retry-fp" {
		t.Fatalf("panel=%#v, want successful Final card persisted after retry", panel)
	}
}
