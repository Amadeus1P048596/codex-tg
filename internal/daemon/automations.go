package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/automation"
	"github.com/mideco-tech/codex-tg/internal/control"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const (
	automationPollInterval = 15 * time.Second
	automationStatePrefix  = "automation.run."
	automationThreadPrefix = "automation.thread."
)

type automationRunState struct {
	SlotUnixMilli int64  `json:"slot_unix_milli,omitempty"`
	Status        string `json:"status"`
	StartedAt     string `json:"started_at,omitempty"`
	UpdatedAt     string `json:"updated_at"`
	ThreadID      string `json:"thread_id,omitempty"`
	TurnID        string `json:"turn_id,omitempty"`
	Error         string `json:"error,omitempty"`
}

func automationRunStateKey(taskID string) string {
	return automationStatePrefix + strings.TrimSpace(taskID)
}

func automationThreadStateKey(threadID string) string {
	return automationThreadPrefix + strings.TrimSpace(threadID)
}

func (s *Service) automationLoop(ctx context.Context) {
	ticker := time.NewTicker(automationPollInterval)
	defer ticker.Stop()
	for {
		s.runAutomationTick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) runAutomationTick(ctx context.Context) {
	if strings.TrimSpace(s.cfg.AutomationsDir) == "" {
		return
	}
	s.automationMu.Lock()
	defer s.automationMu.Unlock()

	now := s.automationNow()
	tasks, err := automation.NewStore(s.cfg.AutomationsDir, s.automationNow).ListTasks()
	if err != nil {
		s.recordAutomationSchedulerError(ctx, fmt.Errorf("list Telegram scheduled tasks: %w", err))
		return
	}
	_ = s.store.SetState(ctx, "automation.scheduler.last_tick_at", now.UTC().Format(time.RFC3339Nano))
	for _, task := range tasks {
		if !strings.EqualFold(strings.TrimSpace(task.Status), "ACTIVE") {
			continue
		}
		due, ok, dueErr := automation.LatestDue(task, now)
		if dueErr != nil {
			s.recordAutomationSchedulerError(ctx, fmt.Errorf("task %s schedule: %w", task.ID, dueErr))
			continue
		}
		if !ok {
			continue
		}
		state := s.loadAutomationRunState(ctx, task.ID)
		if state.SlotUnixMilli >= due.UnixMilli() {
			continue
		}
		target, readyErr := s.automationTarget(ctx)
		if readyErr != nil {
			s.saveAutomationWaitingState(ctx, task.ID, readyErr)
			continue
		}
		if !s.automationLiveReady() {
			s.saveAutomationWaitingState(ctx, task.ID, errors.New("Telegram App Server is not ready"))
			continue
		}

		claim := automationRunState{
			SlotUnixMilli: due.UnixMilli(),
			Status:        "starting",
			StartedAt:     now.UTC().Format(time.RFC3339Nano),
			UpdatedAt:     now.UTC().Format(time.RFC3339Nano),
		}
		if err := s.saveAutomationRunState(ctx, task.ID, claim); err != nil {
			s.recordAutomationSchedulerError(ctx, fmt.Errorf("claim task %s: %w", task.ID, err))
			continue
		}
		threadID, turnID, runErr := s.startAutomationTurn(ctx, task, *target)
		claim.ThreadID = threadID
		claim.TurnID = turnID
		claim.UpdatedAt = s.automationNow().UTC().Format(time.RFC3339Nano)
		if runErr != nil {
			claim.Status = "failed"
			claim.Error = sanitizeDiagnosticString(runErr.Error())
			_ = s.saveAutomationRunState(ctx, task.ID, claim)
			s.recordAutomationSchedulerError(ctx, fmt.Errorf("task %s failed: %w", task.ID, runErr))
			s.notifyAutomationStartFailure(ctx, *target, task, claim.Error)
			continue
		}
		claim.Status = "running"
		claim.Error = ""
		_ = s.saveAutomationRunState(ctx, task.ID, claim)
		_ = s.store.SetState(ctx, "automation.scheduler.last_error", "")
		s.logLifecycle("automation_turn_started", lifecycleFields{
			"task_id": task.ID, "thread_id": threadID, "turn_id": turnID, "slot": due.UTC().Format(time.RFC3339),
		})
	}
}

func (s *Service) automationNow() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) automationLiveReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.liveConnected && !s.liveReleasing && s.live != nil
}

func (s *Service) automationTarget(ctx context.Context) (*model.ObserverTarget, error) {
	target, err := s.currentBackgroundTarget(ctx)
	if err != nil {
		return nil, err
	}
	if target == nil || !target.Enabled {
		return nil, errors.New("no Telegram observer target is configured for scheduled-task results")
	}
	return target, nil
}

func (s *Service) startAutomationTurn(ctx context.Context, task automation.Task, target model.ObserverTarget) (string, string, error) {
	// A scheduled dispatch is live-writer activity just like Telegram input. This
	// prevents the idle-release loop from recycling the session between the due
	// check and ThreadStart.
	s.touchTelegramWriterActivity()
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected && !s.liveReleasing
	s.mu.RUnlock()
	if !connected || live == nil {
		return "", "", errors.New("Telegram App Server is not ready")
	}
	cwd := automationTaskCWD(task, s.cfg.DefaultCWD)
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	started := time.Now()
	threadPayload, err := live.ThreadStart(requestCtx, cwd)
	cancel()
	s.logAppServerCall("ThreadStart", started, err, live, lifecycleFields{"operation": "automation_start", "task_id": task.ID})
	if err != nil {
		return "", "", err
	}
	thread := appserver.ThreadFromPayload(threadPayload)
	if strings.TrimSpace(thread.ID) == "" {
		return "", "", errors.New("scheduled ThreadStart returned no thread id")
	}
	thread.CWD = firstNonEmpty(thread.CWD, cwd)
	thread.Title = firstNonEmpty(strings.TrimSpace(task.Name), thread.Title, thread.ID)
	thread.LastPreview = task.Prompt
	s.markLiveThreadOwned(thread.ID)
	_ = s.setTelegramWriterReleased(ctx, thread.ID, false)
	_ = s.store.SetState(ctx, manualThreadTitleStateKey(thread.ID), thread.Title)
	_ = s.store.SetState(ctx, automationThreadStateKey(thread.ID), task.ID)
	if err := s.store.UpsertThread(ctx, thread); err != nil {
		return thread.ID, "", err
	}
	if admin, ok := live.(control.ThreadAdmin); ok && strings.TrimSpace(task.Name) != "" {
		requestCtx, cancel = context.WithTimeout(ctx, s.cfg.RequestTimeout)
		started = time.Now()
		_, titleErr := admin.ThreadSetName(requestCtx, thread.ID, task.Name)
		cancel()
		s.logAppServerCall("ThreadSetName", started, titleErr, live, lifecycleFields{"operation": "automation_title", "task_id": task.ID, "thread_id": thread.ID})
	}

	options := appserver.TurnStartOptions{
		Model:           strings.TrimSpace(task.Model),
		ReasoningEffort: normalizeReasoningEffort(task.ReasoningEffort),
	}
	requestCtx, cancel = context.WithTimeout(ctx, s.cfg.RequestTimeout)
	started = time.Now()
	turnPayload, err := live.TurnStart(requestCtx, thread.ID, task.Prompt, thread.CWD, options)
	cancel()
	s.logAppServerCall("TurnStart", started, err, live, lifecycleFields{
		"operation": "automation_start", "task_id": task.ID, "thread_id": thread.ID,
		"model": options.Model, "reasoning_effort": options.ReasoningEffort,
	})
	if err != nil {
		return thread.ID, "", err
	}
	turnID := appserverThreadTurnID(turnPayload)
	if turnID == "" {
		return thread.ID, "", errors.New("scheduled TurnStart returned no turn id")
	}
	thread.ActiveTurnID = turnID
	thread.Status = "inProgress"
	thread.UpdatedAt = s.automationNow().UTC().Unix()
	_ = s.store.UpsertThread(ctx, thread)
	s.ensureStartedTurnSnapshot(ctx, &thread, turnID)
	s.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	return thread.ID, turnID, nil
}

func automationTaskCWD(task automation.Task, fallback string) string {
	if cwd := strings.TrimSpace(task.CWD); cwd != "" {
		return filepath.Clean(cwd)
	}
	if projectPath := strings.TrimSpace(task.ProjectID); projectPath != "" && filepath.IsAbs(projectPath) {
		return filepath.Clean(projectPath)
	}
	return strings.TrimSpace(fallback)
}

func (s *Service) loadAutomationRunState(ctx context.Context, taskID string) automationRunState {
	raw, err := s.store.GetState(ctx, automationRunStateKey(taskID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return automationRunState{}
	}
	var state automationRunState
	if json.Unmarshal([]byte(raw), &state) != nil {
		return automationRunState{}
	}
	return state
}

func (s *Service) saveAutomationRunState(ctx context.Context, taskID string, state automationRunState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.store.SetState(ctx, automationRunStateKey(taskID), string(data))
}

func (s *Service) updateAutomationRunFromSnapshot(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) {
	if snapshot == nil || strings.TrimSpace(thread.ID) == "" {
		return
	}
	taskID, err := s.store.GetState(ctx, automationThreadStateKey(thread.ID))
	if err != nil || strings.TrimSpace(taskID) == "" {
		return
	}
	state := s.loadAutomationRunState(ctx, taskID)
	if state.ThreadID != "" && state.ThreadID != thread.ID {
		return
	}
	waiting := snapshot.WaitingOnApproval || snapshot.WaitingOnReply || snapshot.PlanPrompt != nil
	next := notificationStateForStatus(readableStatus(snapshot.LatestTurnStatus, thread.Status), waiting)
	switch next {
	case notificationNeedsInput:
		state.Status = "needs_input"
	case notificationCompleted:
		state.Status = "completed"
		state.Error = ""
	case notificationFailed:
		state.Status = "failed"
	case notificationCancelled:
		state.Status = "cancelled"
	default:
		return
	}
	state.ThreadID = thread.ID
	state.TurnID = firstNonEmpty(snapshot.LatestTurnID, state.TurnID)
	state.UpdatedAt = s.automationNow().UTC().Format(time.RFC3339Nano)
	_ = s.saveAutomationRunState(ctx, taskID, state)
}

func (s *Service) saveAutomationWaitingState(ctx context.Context, taskID string, err error) {
	now := s.automationNow().UTC().Format(time.RFC3339Nano)
	state := automationRunState{Status: "waiting", UpdatedAt: now, Error: sanitizeDiagnosticString(err.Error())}
	_ = s.saveAutomationRunState(ctx, taskID, state)
	s.recordAutomationSchedulerError(ctx, err)
}

func (s *Service) recordAutomationSchedulerError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	message := sanitizeDiagnosticString(err.Error())
	_ = s.store.SetState(ctx, "automation.scheduler.last_error", message)
	s.logLifecycle("automation_scheduler_error", lifecycleFields{"error": message})
}

func (s *Service) notifyAutomationStartFailure(ctx context.Context, target model.ObserverTarget, task automation.Task, message string) {
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return
	}
	text := "⏰ <b>定时任务启动失败</b>\n\n" + html.EscapeString(task.Name)
	if strings.TrimSpace(message) != "" {
		text += "\n" + html.EscapeString(message)
	}
	_, _ = sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, []model.RenderedMessage{{
		Text: text, ParseMode: "HTML", PlainText: "定时任务启动失败\n\n" + task.Name + "\n" + message,
	}}, nil, notifySendOptions())
}
