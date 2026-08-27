package daemon

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const foregroundThreadStatePrefix = "ui.foreground_thread."

func foregroundThreadStateKey(chatID, topicID int64) string {
	return foregroundThreadStatePrefix + model.ChatKey(chatID, topicID)
}

func backgroundThreadNoticeStateKey(chatID, topicID int64, threadID string) string {
	return "ui.background_notice." + model.ChatKey(chatID, topicID) + "." + strings.TrimSpace(threadID)
}

func foregroundThreadNoticeStateKey(chatID, topicID int64, threadID string) string {
	return "ui.foreground_terminal_notice." + model.ChatKey(chatID, topicID) + "." + strings.TrimSpace(threadID)
}

func (s *Service) foregroundThreadID(ctx context.Context, chatID, topicID int64) (string, error) {
	value, err := s.store.GetState(ctx, foregroundThreadStateKey(chatID, topicID))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (s *Service) ensureForegroundThread(ctx context.Context, target model.ObserverTarget, threadID string) (bool, error) {
	threadID = strings.TrimSpace(threadID)
	current, err := s.foregroundThreadID(ctx, target.ChatID, target.TopicID)
	if err != nil {
		return false, err
	}
	if current == "" {
		if err := s.store.SetState(ctx, foregroundThreadStateKey(target.ChatID, target.TopicID), threadID); err != nil {
			return false, err
		}
		if err := s.clearInboxItem(ctx, target.ChatID, target.TopicID, threadID); err != nil {
			return false, err
		}
		current = threadID
	}
	return current == threadID, nil
}

func (s *Service) activateForegroundThread(ctx context.Context, target model.ObserverTarget, threadID string, bind bool) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	s.panelMu.Lock()
	defer s.panelMu.Unlock()

	current, err := s.foregroundThreadID(ctx, target.ChatID, target.TopicID)
	if err != nil {
		return err
	}
	if current != "" && current != threadID {
		s.hideForegroundWorkingCardLocked(ctx, target, current)
	}
	if err := s.store.SetState(ctx, foregroundThreadStateKey(target.ChatID, target.TopicID), threadID); err != nil {
		return err
	}
	if err := s.clearInboxItem(ctx, target.ChatID, target.TopicID, threadID); err != nil {
		return err
	}
	if bind {
		if err := s.store.SetBinding(ctx, target.ChatID, target.TopicID, threadID, model.BindingModeBound); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) hideForegroundWorkingCardLocked(ctx context.Context, target model.ObserverTarget, threadID string) {
	panel, err := s.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, threadID)
	if err != nil || panel == nil || panel.SummaryMessageID == 0 || isTerminalStatus(panel.Status) {
		return
	}
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return
	}
	messageID := panel.SummaryMessageID
	if err := sender.DeleteMessage(ctx, target.ChatID, target.TopicID, messageID); err != nil {
		s.setError(ctx, fmt.Errorf("hide previous foreground card: %w", err))
		return
	}
	panel.SummaryMessageID = 0
	panel.PlanPromptMessageID = 0
	_ = s.store.UpdateThreadPanelMessages(ctx, panel.ID, 0, panel.ToolMessageID, panel.OutputMessageID)
	_ = s.store.UpdateThreadPanelPlanPrompt(ctx, panel.ID, 0, panel.LastPlanPromptFP)
}

func (s *Service) handleBackgroundThreadSnapshotLocked(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, pending *model.PendingApproval) {
	s.hideForegroundWorkingCardLocked(ctx, target, thread.ID)
	waiting := pending != nil || snapshot.WaitingOnApproval || snapshot.WaitingOnReply || snapshot.PlanPrompt != nil
	state := notificationStateForStatus(readableStatus(snapshot.LatestTurnStatus, thread.Status), waiting)
	if !notificationStateIsTerminal(state) && state != notificationNeedsInput {
		_ = s.clearInboxItem(ctx, target.ChatID, target.TopicID, thread.ID)
		return
	}
	fingerprint := hashStrings(thread.ID, snapshot.LatestTurnID, string(state), snapshot.LatestFinalFP, pendingRequestID(pending))
	if err := s.saveInboxItem(ctx, target.ChatID, target.TopicID, sessionInboxItem{
		ThreadID: thread.ID, TurnID: snapshot.LatestTurnID, Title: threadSelectionTitle(thread),
		State: state, UpdatedAt: s.currentTime().Unix(), Fingerprint: fingerprint,
	}); err != nil {
		s.setError(ctx, fmt.Errorf("persist background session inbox item: %w", err))
	}
	stateKey := backgroundThreadNoticeStateKey(target.ChatID, target.TopicID, thread.ID)
	if previous, err := s.store.GetState(ctx, stateKey); err == nil && strings.TrimSpace(previous) == fingerprint {
		return
	}
	title := threadSelectionTitle(thread)
	label := backgroundThreadStateLabel(state)
	message := model.RenderedMessage{
		Text:      html.EscapeString(s.visualMarker(ctx, thread.ID)) + " <b>" + html.EscapeString(title) + "</b> " + label,
		ParseMode: "HTML",
		PlainText: s.visualMarker(ctx, thread.ID) + " " + title + " " + label,
	}
	buttons := [][]model.ButtonSpec{{
		s.callbackButton(ctx, "切换至该会话", "switch_thread", thread.ID, snapshot.LatestTurnID, "", nil),
	}}
	ids, err := sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, []model.RenderedMessage{message}, buttons, notifySendOptions())
	if err != nil {
		s.setError(ctx, fmt.Errorf("send background thread notice: %w", err))
		return
	}
	messageID := lastMessageID(ids)
	if messageID != 0 {
		_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
			ChatID: target.ChatID, TopicID: target.TopicID, MessageID: messageID,
			ThreadID: thread.ID, TurnID: snapshot.LatestTurnID, EventID: "background_notice:" + fingerprint,
			CreatedAt: model.NowString(),
		})
	}
	_ = s.store.SetState(ctx, stateKey, fingerprint)
}

func pendingRequestID(pending *model.PendingApproval) string {
	if pending == nil {
		return ""
	}
	return pending.RequestID
}

func backgroundThreadStateLabel(state notificationState) string {
	switch state {
	case notificationCompleted:
		return "已完成"
	case notificationFailed:
		return "失败"
	case notificationCancelled:
		return "已取消"
	case notificationNeedsInput:
		return "需要输入"
	default:
		return "状态已更新"
	}
}

func (s *Service) sendForegroundTerminalNotice(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) error {
	if sender == nil || snapshot == nil {
		return nil
	}
	state := notificationStateForStatus(readableStatus(snapshot.LatestTurnStatus, thread.Status), false)
	if !notificationStateIsTerminal(state) {
		return nil
	}
	fingerprint := hashStrings(thread.ID, snapshot.LatestTurnID, string(state), snapshot.LatestFinalFP)
	stateKey := foregroundThreadNoticeStateKey(target.ChatID, target.TopicID, thread.ID)
	if previous, err := s.store.GetState(ctx, stateKey); err == nil && strings.TrimSpace(previous) == fingerprint {
		return nil
	}
	title := threadSelectionTitle(thread)
	label := backgroundThreadStateLabel(state)
	message := model.RenderedMessage{
		Text:      html.EscapeString(s.visualMarker(ctx, thread.ID)) + " <b>" + html.EscapeString(title) + "</b> " + label,
		ParseMode: "HTML",
		PlainText: s.visualMarker(ctx, thread.ID) + " " + title + " " + label,
	}
	ids, err := sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, []model.RenderedMessage{message}, nil, notifySendOptions())
	if err != nil {
		return fmt.Errorf("send foreground terminal notice: %w", err)
	}
	messageID := lastMessageID(ids)
	if messageID != 0 {
		_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
			ChatID: target.ChatID, TopicID: target.TopicID, MessageID: messageID,
			ThreadID: thread.ID, TurnID: snapshot.LatestTurnID, EventID: "foreground_terminal_notice:" + fingerprint,
			CreatedAt: model.NowString(),
		})
	}
	return s.store.SetState(ctx, stateKey, fingerprint)
}
