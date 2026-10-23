package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	automationDontNotify   = "DONT_NOTIFY"
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

func (s *Service) isAutomationExecutionThread(ctx context.Context, threadID string) bool {
	if strings.TrimSpace(threadID) == "" {
		return false
	}
	taskID, err := s.store.GetState(ctx, automationThreadStateKey(threadID))
	return err == nil && strings.TrimSpace(taskID) != ""
}

// Only the currently claimed occurrence may borrow the transient-interrupt
// protection. Older hidden threads remain pollable but cannot publish anew.
func (s *Service) isCurrentAutomationTurn(ctx context.Context, threadID, turnID string) bool {
	taskID, err := s.store.GetState(ctx, automationThreadStateKey(threadID))
	if err != nil || taskID == "" || turnID == "" {
		return false
	}
	run := s.loadAutomationRunState(ctx, taskID)
	return run.ThreadID == threadID && run.TurnID == turnID
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
			s.notifyAutomationStartFailure(ctx, task, claim)
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
		return nil, errors.New("no Telegram push target is configured for scheduled-task results")
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

	options := s.turnStartOptions(ctx, "", nil)
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
	if state.TurnID != "" && state.TurnID != snapshot.LatestTurnID {
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

// syncAutomationPush owns presentation for scheduler-created threads. Their
// internal App Server threads remain isolated execution contexts, but they do
// not create or update normal Telegram conversation cards.
func (s *Service) syncAutomationPush(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil || strings.TrimSpace(thread.ID) == "" {
		return false
	}
	taskID, err := s.store.GetState(ctx, automationThreadStateKey(thread.ID))
	if err != nil || strings.TrimSpace(taskID) == "" {
		return false
	}
	run := s.loadAutomationRunState(ctx, taskID)
	if (run.ThreadID != "" && run.ThreadID != thread.ID) ||
		(run.TurnID != "" && run.TurnID != snapshot.LatestTurnID) {
		return true
	}
	// Poll and live projections can disagree briefly. Do not let an ambiguous
	// interrupted projection escape as a push before the normal terminal gate.
	decision, gateErr := s.decideTelegramOriginEmptyInterruptedTerminal(ctx, snapshot, s.automationNow())
	if gateErr != nil || decision.Action == terminalGateDefer {
		return true
	}
	s.updateAutomationRunFromSnapshot(ctx, thread, snapshot)
	state := notificationStateForStatus(
		readableStatus(snapshot.LatestTurnStatus, thread.Status),
		snapshot.WaitingOnApproval || snapshot.WaitingOnReply || snapshot.PlanPrompt != nil,
	)
	if state == notificationRunning {
		return true
	}
	task, found, err := s.automationTask(taskID)
	if err != nil {
		s.recordAutomationSchedulerError(ctx, fmt.Errorf("load task %s for push: %w", taskID, err))
		return true
	}
	if !found {
		s.recordAutomationSchedulerError(ctx, fmt.Errorf("load task %s for push: task not found", taskID))
		return true
	}
	if state == notificationCompleted && strings.EqualFold(strings.TrimSpace(task.NotificationPolicy), "failed_runs_only") {
		return true
	}
	if state == notificationCompleted && snapshot != nil && strings.TrimSpace(snapshot.LatestFinalText) == automationDontNotify {
		return true
	}
	run = s.loadAutomationRunState(ctx, taskID)
	body := automationPushBody(state, snapshot, run)
	conversationTitle := s.automationConversationTitle(ctx, task)
	phase := "terminal"
	if state == notificationNeedsInput {
		phase = "needs_input"
	}
	// Content fingerprints vary between live events and poll snapshots. They
	// are not a delivery identity: one occurrence gets one terminal push.
	idempotencyKey := fmt.Sprintf("automation:%s:%d:%s:%s:%s", task.ID, run.SlotUnixMilli, thread.ID, firstNonEmpty(snapshot.LatestTurnID, run.TurnID), phase)
	_, err = s.publishPush(ctx, PushRequest{
		Source:            "codex-tg 定时任务",
		Title:             compactPushText("定时任务 · "+task.Name, pushTitleMaxRunes),
		Body:              compactPushBody(body),
		ConversationTitle: compactPushText(conversationTitle, pushConversationMaxRunes),
		Severity:          "normal",
		IdempotencyKey:    idempotencyKey,
	})
	if err != nil {
		s.recordAutomationSchedulerError(ctx, fmt.Errorf("push task %s result: %w", task.ID, err))
	}
	return true
}

func (s *Service) automationTask(taskID string) (automation.Task, bool, error) {
	tasks, err := automation.NewStore(s.cfg.AutomationsDir, s.automationNow).ListTasks()
	if err != nil {
		return automation.Task{}, false, err
	}
	for _, task := range tasks {
		if task.ID == strings.TrimSpace(taskID) {
			return task, true, nil
		}
	}
	return automation.Task{}, false, nil
}

func (s *Service) automationConversationTitle(ctx context.Context, task automation.Task) string {
	if strings.TrimSpace(task.SourceThreadID) != "" {
		if sourceThread, err := s.store.GetThread(ctx, task.SourceThreadID); err == nil && sourceThread != nil {
			if title := threadSelectionTitle(*sourceThread); title != "" {
				return title
			}
		}
	}
	return firstNonEmpty(task.SourceThreadTitle, "未关联会话")
}

func automationPushBody(state notificationState, snapshot *appserver.ThreadReadSnapshot, run automationRunState) string {
	if snapshot == nil {
		return "定时任务没有返回可读取的结果。"
	}
	switch state {
	case notificationCompleted:
		return firstNonEmpty(snapshot.LatestFinalText, "定时任务已完成。")
	case notificationNeedsInput:
		if snapshot.PlanPrompt != nil {
			return firstNonEmpty(snapshot.PlanPrompt.Question, "定时任务正在等待输入。")
		}
		return "定时任务正在等待输入或授权。"
	case notificationFailed:
		return firstNonEmpty(snapshot.LatestFinalText, run.Error, snapshot.LatestProgressText, "定时任务执行失败。")
	case notificationCancelled:
		return firstNonEmpty(snapshot.LatestFinalText, "定时任务已取消。")
	default:
		return firstNonEmpty(snapshot.LatestProgressText, "定时任务状态已更新。")
	}
}

func compactPushBody(value string) string {
	const suffix = "\n\n…（结果已截断）"
	value = strings.TrimSpace(value)
	limit := pushMessageBodyMaxRunes - len([]rune(suffix))
	if len([]rune(value)) <= pushMessageBodyMaxRunes {
		return value
	}
	return strings.TrimSpace(string([]rune(value)[:limit])) + suffix
}

func compactPushText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
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

func (s *Service) notifyAutomationStartFailure(ctx context.Context, task automation.Task, run automationRunState) {
	_, _ = s.publishPush(ctx, PushRequest{
		Source:            "codex-tg 定时任务",
		Title:             compactPushText("定时任务启动失败 · "+task.Name, pushTitleMaxRunes),
		Body:              compactPushBody(firstNonEmpty(run.Error, "无法启动定时任务。")),
		ConversationTitle: compactPushText(s.automationConversationTitle(ctx, task), pushConversationMaxRunes),
		Severity:          "normal",
		IdempotencyKey:    fmt.Sprintf("automation:%s:%d:start-failed", task.ID, run.SlotUnixMilli),
	})
}

func (s *Service) captureAutomationOriginFromEvent(ctx context.Context, event appserver.Event, thread model.Thread) {
	if strings.TrimSpace(s.cfg.AutomationsDir) == "" || event.Channel != "notification" || !strings.EqualFold(strings.TrimSpace(event.Method), "item/completed") {
		return
	}
	item, _ := event.Params["item"].(map[string]any)
	if !strings.EqualFold(payloadMapString(item, "type"), "mcpToolCall") ||
		!strings.EqualFold(payloadMapString(item, "tool"), "automation_update") ||
		!strings.EqualFold(payloadMapString(item, "status"), "completed") {
		return
	}
	arguments, _ := item["arguments"].(map[string]any)
	mode := strings.ToLower(payloadMapString(arguments, "mode"))
	if mode != "create" && mode != "update" {
		return
	}
	taskID := payloadMapString(arguments, "id")
	if taskID == "" {
		taskID = automationIDFromMCPResult(item["result"])
	}
	threadID := firstNonEmpty(thread.ID, threadIDFromEvent(event))
	title := strings.TrimSpace(thread.Title)
	if manualTitle, err := s.store.GetState(ctx, manualThreadTitleStateKey(threadID)); err == nil {
		title = firstNonEmpty(manualTitle, title)
	}
	title = compactPushText(firstNonEmpty(title, "未命名会话"), pushConversationMaxRunes)
	if taskID == "" || threadID == "" {
		return
	}
	if err := automation.NewStore(s.cfg.AutomationsDir, s.automationNow).SetOriginIfMissing(taskID, threadID, title); err != nil {
		s.recordAutomationSchedulerError(ctx, fmt.Errorf("attach task origin: %w", err))
	}
}

func automationIDFromMCPResult(result any) string {
	resultMap, _ := result.(map[string]any)
	if id := payloadMapString(resultMap, "id"); id != "" {
		return id
	}
	content, _ := resultMap["content"].([]any)
	for _, raw := range content {
		entry, _ := raw.(map[string]any)
		text := payloadMapString(entry, "text")
		if text == "" {
			continue
		}
		var decoded map[string]any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			if id := payloadMapString(decoded, "id"); id != "" {
				return id
			}
		}
	}
	return ""
}
