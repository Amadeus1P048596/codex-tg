package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/automation"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func scheduledPushFixture(t *testing.T) (*Service, string, model.Thread, *recordingSender) {
	t.Helper()
	s := newTestService(t)
	ctx := context.Background()
	s.cfg.AutomationsDir = filepath.Join(s.cfg.Paths.Home, "automations")
	task, err := automation.NewStore(s.cfg.AutomationsDir, time.Now).Apply(map[string]any{"mode": "create", "name": "Scheduled check", "prompt": "Check", "rrule": "FREQ=HOURLY;INTERVAL=1", "status": "ACTIVE", "kind": "cron", "projectId": nil, "executionEnvironment": "local"})
	if err != nil {
		t.Fatal(err)
	}
	id := task["id"].(string)
	thread := model.Thread{ID: "scheduled-run", Status: "inProgress", ActiveTurnID: "scheduled-turn"}
	if err = s.store.UpsertThread(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if err = s.store.SetState(ctx, automationThreadStateKey(thread.ID), id); err != nil {
		t.Fatal(err)
	}
	if err = s.saveAutomationRunState(ctx, id, automationRunState{SlotUnixMilli: 123, Status: "running", ThreadID: thread.ID, TurnID: thread.ActiveTurnID}); err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	s.SetSender(sender)
	if err = s.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatal(err)
	}
	return s, id, thread, sender
}

func TestScheduledTransientInterruptedThenSilentCompletion(t *testing.T) {
	s, id, thread, sender := scheduledPushFixture(t)
	ctx := context.Background()
	snap := appserver.ThreadReadSnapshot{Thread: thread, LatestTurnID: thread.ActiveTurnID, LatestTurnStatus: "interrupted"}
	for _, fp := range []string{"one", "two", "three"} {
		snap.LatestProgressFP = fp
		s.syncAutomationPush(ctx, thread, &snap)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("transient interruption sent %d messages", len(sender.messages))
	}
	if s.loadAutomationRunState(ctx, id).Status != "running" {
		t.Fatal("transient snapshot changed run state")
	}
	snap.LatestTurnStatus = "completed"
	snap.LatestFinalText = "DONT_NOTIFY"
	s.syncAutomationPush(ctx, thread, &snap)
	if len(sender.messages) != 0 {
		t.Fatal("silent completion sent a message")
	}
	if s.loadAutomationRunState(ctx, id).Status != "completed" {
		t.Fatal("completion not recorded")
	}
}

func TestScheduledTerminalPushIgnoresChangingFingerprint(t *testing.T) {
	s, _, thread, sender := scheduledPushFixture(t)
	ctx := context.Background()
	snap := appserver.ThreadReadSnapshot{Thread: thread, LatestTurnID: thread.ActiveTurnID, LatestTurnStatus: "completed", LatestFinalText: "Daily summary"}
	for _, fp := range []string{"live-fp", "poll-fp", "recovered-fp"} {
		snap.LatestFinalFP = fp
		s.syncAutomationPush(ctx, thread, &snap)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("got %d terminal messages, want one", len(sender.messages))
	}
}

func TestScheduledPushRejectsOldRun(t *testing.T) {
	s, id, thread, sender := scheduledPushFixture(t)
	ctx := context.Background()
	newer := automationRunState{SlotUnixMilli: 456, Status: "running", ThreadID: "new-thread", TurnID: "new-turn"}
	if err := s.saveAutomationRunState(ctx, id, newer); err != nil {
		t.Fatal(err)
	}
	snap := appserver.ThreadReadSnapshot{Thread: thread, LatestTurnID: thread.ActiveTurnID, LatestTurnStatus: "completed", LatestFinalText: "Old summary"}
	s.syncAutomationPush(ctx, thread, &snap)
	if len(sender.messages) != 0 {
		t.Fatal("old run published against new slot")
	}
}

func TestScheduledInterruptedGateExpiresOrRecovers(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(map[bool]string{false: "expires", true: "recovers"}[recover], func(t *testing.T) {
			s, _, thread, _ := scheduledPushFixture(t)
			ctx := context.Background()
			now := time.Now().UTC()
			snap := terminalGateTestSnapshot(thread.ID, thread.ActiveTurnID, "interrupted")
			decision, err := s.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snap, now)
			if err != nil || decision.Action != terminalGateDefer {
				t.Fatalf("initial decision=%+v err=%v", decision, err)
			}
			want := terminalGateAccept
			later := now.Add(decision.Grace + time.Second)
			if recover {
				snap.LatestTurnStatus = "completed"
				want = terminalGateRecover
				later = now.Add(time.Second)
			}
			decision, err = s.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snap, later)
			if err != nil || decision.Action != want {
				t.Fatalf("later decision=%+v err=%v", decision, err)
			}
		})
	}
}
