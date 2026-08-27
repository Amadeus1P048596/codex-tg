package daemon

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/control"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const (
	telegramThreadTitleMaxRunes  = 120
	archivedThreadsPageSize      = 10
	manualThreadTitleStatePrefix = "ui.thread_title.manual."
)

var (
	errThreadAdminNotReady    = errors.New("live app-server session is not ready")
	errThreadAdminUnsupported = errors.New("live app-server does not support thread administration")
)

func (s *Service) currentTelegramThread(ctx context.Context, chatID, topicID int64) (*model.Thread, error) {
	threadID, err := s.foregroundThreadID(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	if threadID == "" {
		binding, bindingErr := s.store.GetBinding(ctx, chatID, topicID)
		if bindingErr != nil {
			return nil, bindingErr
		}
		if binding != nil {
			threadID = strings.TrimSpace(binding.ThreadID)
		}
	}
	if threadID == "" {
		return nil, nil
	}
	thread, err := s.store.GetThread(ctx, threadID)
	if err != nil || thread == nil || thread.Archived {
		return nil, err
	}
	return thread, nil
}

func (s *Service) renameCurrentThread(ctx context.Context, chatID, topicID int64, requestedTitle string) (*DirectResponse, error) {
	title := compactThreadSelectionText(requestedTitle)
	if title == "" {
		return &DirectResponse{Text: "用法：/title 新的会话标题"}, nil
	}
	if len([]rune(title)) > telegramThreadTitleMaxRunes {
		return &DirectResponse{Text: fmt.Sprintf("会话标题最多 %d 个字符。", telegramThreadTitleMaxRunes)}, nil
	}
	thread, err := s.currentTelegramThread(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return &DirectResponse{Text: "当前没有已选择的会话，请先使用 /threads 或 /newthread。"}, nil
	}
	if err := s.callLiveThreadAdmin(ctx, "thread_set_name", thread.ID, func(requestCtx context.Context, admin control.ThreadAdmin) error {
		_, callErr := admin.ThreadSetName(requestCtx, thread.ID, title)
		return callErr
	}); err != nil {
		return threadAdminFailureResponse(err), nil
	}
	if err := s.store.SetState(ctx, manualThreadTitleStateKey(thread.ID), title); err != nil {
		return nil, err
	}
	thread.Title = title
	thread.UpdatedAt = s.currentTime().Unix()
	if err := s.store.UpsertThread(ctx, *thread); err != nil {
		return nil, err
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(chatID, topicID), ChatID: chatID, TopicID: topicID, Enabled: true}
	s.refreshRenamedThreadCard(ctx, target, *thread)
	return &DirectResponse{Text: "已将当前会话标题修改为：\n" + title, ThreadID: thread.ID}, nil
}

func (s *Service) currentThreadOverview(ctx context.Context, chatID, topicID int64) (*DirectResponse, error) {
	thread, err := s.currentTelegramThread(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return &DirectResponse{Text: "当前没有已选择的会话，请先使用 /threads、/newchat 或 /newthread。"}, nil
	}
	status := currentThreadStatusLabel(*thread, nil)
	if _, snapshot, snapshotErr := s.loadThreadPanelSnapshot(ctx, thread.ID); snapshotErr == nil && snapshot != nil {
		status = currentThreadStatusLabel(*thread, snapshot)
	}
	text := strings.Join([]string{
		"当前会话",
		"",
		s.visualMarker(ctx, thread.ID) + " " + threadSelectionTitle(*thread),
		"状态：" + status,
		"T:" + visualShortID(thread.ID),
	}, "\n")
	return &DirectResponse{Text: text, ThreadID: thread.ID}, nil
}

func currentThreadStatusLabel(thread model.Thread, snapshot *appserver.ThreadReadSnapshot) string {
	if snapshot != nil {
		if snapshot.WaitingOnApproval || snapshot.WaitingOnReply || snapshot.PlanPrompt != nil {
			return "需要输入"
		}
		switch notificationStateForStatus(snapshot.LatestTurnStatus, false) {
		case notificationCompleted:
			return "已完成"
		case notificationFailed:
			return "失败"
		case notificationCancelled:
			return "已取消"
		}
		if strings.EqualFold(strings.TrimSpace(snapshot.LatestTurnStatus), "inProgress") {
			return "处理中"
		}
	}
	normalized := strings.ToLower(strings.TrimSpace(thread.Status))
	switch {
	case normalized == "idle":
		return "空闲"
	case normalized == "notloaded":
		return "不可用"
	case strings.Contains(normalized, "fail") || strings.Contains(normalized, "error"):
		return "失败"
	case strings.Contains(normalized, "interrupt") || strings.Contains(normalized, "cancel"):
		return "已取消"
	case strings.Contains(normalized, "complete") || strings.Contains(normalized, "success"):
		return "已完成"
	case strings.Contains(normalized, "active") || strings.Contains(normalized, "progress") || strings.Contains(normalized, "running"):
		return "处理中"
	default:
		return "空闲"
	}
}

func (s *Service) archiveCurrentThreadPrompt(ctx context.Context, chatID, topicID int64) (*DirectResponse, error) {
	thread, err := s.currentTelegramThread(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return &DirectResponse{Text: "当前没有已选择的会话，请先使用 /threads 或 /newthread。"}, nil
	}
	if blocked, response := s.archiveBlockedResponse(ctx, *thread); blocked {
		return response, nil
	}
	title := threadSelectionTitle(*thread)
	buttons := [][]model.ButtonSpec{{
		s.callbackButton(ctx, "确认归档", "archive_confirm", thread.ID, "", "", nil),
		s.callbackButton(ctx, "取消", "archive_cancel", thread.ID, "", "", nil),
	}}
	return &DirectResponse{
		Text:     "确认归档当前会话？\n\n" + s.visualMarker(ctx, thread.ID) + " " + title + "\n\n归档后可通过 /unarchive 恢复。",
		Buttons:  buttons,
		ThreadID: thread.ID,
	}, nil
}

func (s *Service) confirmArchiveThread(ctx context.Context, chatID, topicID, messageID int64, route *model.CallbackRoute) (*DirectResponse, error) {
	if route == nil {
		return &DirectResponse{CallbackText: "归档确认已失效。"}, nil
	}
	current, err := s.currentTelegramThread(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.ID != route.ThreadID {
		_ = s.store.ExpireCallbackRoute(ctx, route.Token)
		return &DirectResponse{CallbackText: "会话已切换，请重新发送 /archive。"}, nil
	}
	if blocked, response := s.archiveBlockedResponse(ctx, *current); blocked {
		_ = s.store.ExpireCallbackRoute(ctx, route.Token)
		s.editThreadAdminResult(ctx, chatID, topicID, messageID, html.EscapeString(response.Text), response.Buttons)
		return &DirectResponse{CallbackText: "当前任务尚未结束，不能归档。"}, nil
	}
	if err := s.callLiveThreadAdmin(ctx, "thread_archive", current.ID, func(requestCtx context.Context, admin control.ThreadAdmin) error {
		_, callErr := admin.ThreadArchive(requestCtx, current.ID)
		return callErr
	}); err != nil {
		return threadAdminFailureResponse(err), nil
	}
	current.Archived = true
	current.UpdatedAt = s.currentTime().Unix()
	if err := s.store.UpsertThread(ctx, *current); err != nil {
		return nil, err
	}
	if binding, bindingErr := s.store.GetBinding(ctx, chatID, topicID); bindingErr == nil && binding != nil && binding.ThreadID == current.ID {
		_ = s.store.ClearBinding(ctx, chatID, topicID)
	}
	if foreground, foregroundErr := s.foregroundThreadID(ctx, chatID, topicID); foregroundErr == nil && foreground == current.ID {
		_ = s.store.SetState(ctx, foregroundThreadStateKey(chatID, topicID), "")
	}
	s.mu.Lock()
	delete(s.liveOwnedThreads, current.ID)
	s.mu.Unlock()
	_ = s.setTelegramWriterReleased(ctx, current.ID, true)
	_ = s.clearInboxItem(ctx, chatID, topicID, current.ID)
	_ = s.store.ExpireCallbackRoute(ctx, route.Token)
	buttons := [][]model.ButtonSpec{{
		s.callbackButton(ctx, "切换其他会话", "home_threads", "", "", "", nil),
		s.callbackButton(ctx, "新建会话", "home_new_menu", "", "", "", nil),
	}}
	s.editThreadAdminResult(ctx, chatID, topicID, messageID, "✅ <b>已归档</b>\n\n"+html.EscapeString(threadSelectionTitle(*current))+"\n\n当前没有活动会话。", buttons)
	return &DirectResponse{CallbackText: "已归档当前会话。"}, nil
}

func (s *Service) cancelArchiveThread(ctx context.Context, chatID, topicID, messageID int64, route *model.CallbackRoute) (*DirectResponse, error) {
	if route != nil {
		_ = s.store.ExpireCallbackRoute(ctx, route.Token)
	}
	s.editThreadAdminResult(ctx, chatID, topicID, messageID, "⚪️ <b>已取消归档</b>", nil)
	return &DirectResponse{CallbackText: "已取消归档。"}, nil
}

func (s *Service) archivedThreadsOverview(ctx context.Context) (*DirectResponse, error) {
	return s.archivedThreadsPage(ctx, "", 1, nil)
}

func (s *Service) archivedThreadsPage(ctx context.Context, cursor string, page int, history []string) (*DirectResponse, error) {
	result, err := s.listArchivedThreads(ctx, cursor)
	if err != nil {
		if errors.Is(err, errThreadAdminNotReady) || errors.Is(err, errThreadAdminUnsupported) {
			return threadAdminFailureResponse(err), nil
		}
		return nil, err
	}
	threads := appserver.ThreadsFromList(result)
	buttons := make([][]model.ButtonSpec, 0, len(threads)+1)
	for index := range threads {
		threads[index].Archived = true
		if existing, getErr := s.store.GetThread(ctx, threads[index].ID); getErr == nil && existing != nil {
			threads[index] = s.mergeRuntimeThreadMetadata(ctx, threads[index], *existing)
			threads[index].Archived = true
		}
		_ = s.store.UpsertThread(ctx, threads[index])
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, threadSelectionTitle(threads[index]), "unarchive_thread", threads[index].ID, "", "", nil),
		})
	}
	nextCursor := payloadMapString(result, "nextCursor")
	nav := make([]model.ButtonSpec, 0, 2)
	if len(history) > 0 {
		previousCursor := history[len(history)-1]
		nav = append(nav, s.callbackButton(ctx, "‹ 上一页", "unarchive_page", "", "", "", map[string]any{
			"cursor": previousCursor, "page": maxInt(1, page-1), "history": append([]string(nil), history[:len(history)-1]...),
		}))
	}
	if nextCursor != "" {
		nextHistory := append(append([]string(nil), history...), cursor)
		nav = append(nav, s.callbackButton(ctx, "下一页 ›", "unarchive_page", "", "", "", map[string]any{
			"cursor": nextCursor, "page": page + 1, "history": nextHistory,
		}))
	}
	if len(nav) > 0 {
		buttons = append(buttons, nav)
	}
	text := fmt.Sprintf("已归档会话 · 第 %d 页\n点击标题恢复会话。", maxInt(1, page))
	if len(threads) == 0 {
		text = "已归档会话\n暂无已归档会话。"
	}
	return &DirectResponse{Text: text, Buttons: buttons}, nil
}

func (s *Service) handleArchivedThreadsPage(ctx context.Context, chatID, topicID, messageID int64, payload map[string]any) (*DirectResponse, error) {
	page := maxInt(1, payloadMapInt(payload, "page"))
	response, err := s.archivedThreadsPage(ctx, payloadMapString(payload, "cursor"), page, payloadMapStringSlice(payload, "history"))
	if err != nil {
		return nil, err
	}
	response.CallbackText = fmt.Sprintf("第 %d 页", page)
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender != nil && messageID != 0 && strings.TrimSpace(response.Text) != "" {
		if err := sender.EditMessage(ctx, chatID, topicID, messageID, response.Text, response.Buttons); err == nil {
			return &DirectResponse{CallbackText: response.CallbackText}, nil
		}
	}
	return response, nil
}

func (s *Service) unarchiveThread(ctx context.Context, chatID, topicID, messageID int64, route *model.CallbackRoute) (*DirectResponse, error) {
	if route == nil || strings.TrimSpace(route.ThreadID) == "" {
		return &DirectResponse{CallbackText: "恢复按钮已失效。"}, nil
	}
	thread, err := s.store.GetThread(ctx, route.ThreadID)
	if err != nil {
		return nil, err
	}
	if err := s.callLiveThreadAdmin(ctx, "thread_unarchive", route.ThreadID, func(requestCtx context.Context, admin control.ThreadAdmin) error {
		_, callErr := admin.ThreadUnarchive(requestCtx, route.ThreadID)
		return callErr
	}); err != nil {
		return threadAdminFailureResponse(err), nil
	}
	if thread == nil {
		thread = &model.Thread{ID: route.ThreadID, Title: route.ThreadID}
	}
	thread.Archived = false
	thread.UpdatedAt = s.currentTime().Unix()
	if err := s.store.UpsertThread(ctx, *thread); err != nil {
		return nil, err
	}
	_ = s.store.ExpireCallbackRoute(ctx, route.Token)
	buttons := [][]model.ButtonSpec{
		{s.callbackButton(ctx, "切换至该会话", "switch_thread", thread.ID, "", "", nil)},
		{s.callbackButton(ctx, "继续查看归档", "unarchive_page", "", "", "", map[string]any{"cursor": "", "page": 1, "history": []string{}})},
	}
	s.editThreadAdminResult(ctx, chatID, topicID, messageID, "✅ <b>已恢复</b>\n\n"+html.EscapeString(threadSelectionTitle(*thread)), buttons)
	return &DirectResponse{CallbackText: "已恢复会话。"}, nil
}

func (s *Service) listArchivedThreads(ctx context.Context, cursor string) (map[string]any, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return nil, errThreadAdminNotReady
	}
	admin, ok := live.(control.ThreadAdmin)
	if !ok {
		return nil, errThreadAdminUnsupported
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	started := time.Now()
	result, err := admin.ThreadListArchived(requestCtx, archivedThreadsPageSize, strings.TrimSpace(cursor))
	s.logAppServerCall("ThreadList", started, err, live, lifecycleFields{"operation": "list_archived", "archived": true})
	return result, err
}

func (s *Service) callLiveThreadAdmin(ctx context.Context, operation, threadID string, call func(context.Context, control.ThreadAdmin) error) error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return errThreadAdminNotReady
	}
	admin, ok := live.(control.ThreadAdmin)
	if !ok {
		return errThreadAdminUnsupported
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	started := time.Now()
	err := call(requestCtx, admin)
	s.logAppServerCall("ThreadAdmin", started, err, live, lifecycleFields{"operation": operation, "thread_id": threadID})
	return err
}

func threadAdminFailureResponse(err error) *DirectResponse {
	switch {
	case errors.Is(err, errThreadAdminNotReady):
		return &DirectResponse{Text: "会话服务暂未就绪，请使用 /status 检查连接。"}
	case errors.Is(err, errThreadAdminUnsupported):
		return &DirectResponse{Text: "当前 Codex App Server 不支持这项会话操作。"}
	default:
		return &DirectResponse{Text: "会话操作失败，请稍后重试或使用 /status 检查连接。"}
	}
}

func (s *Service) editThreadAdminResult(ctx context.Context, chatID, topicID, messageID int64, text string, buttons [][]model.ButtonSpec) {
	if messageID == 0 {
		return
	}
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return
	}
	rendered := model.RenderedMessage{Text: text, ParseMode: "HTML", PlainText: strings.TrimSpace(stripSimpleHTML(text))}
	if err := sender.EditRenderedMessage(ctx, chatID, topicID, messageID, rendered, buttons); err != nil {
		s.setError(ctx, fmt.Errorf("edit thread admin result: %w", err))
	}
}

func stripSimpleHTML(value string) string {
	replacer := strings.NewReplacer("<b>", "", "</b>", "", "<i>", "", "</i>", "", "<code>", "", "</code>", "")
	return html.UnescapeString(replacer.Replace(value))
}

func payloadMapStringSlice(values map[string]any, key string) []string {
	if values == nil {
		return nil
	}
	switch typed := values[key].(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func manualThreadTitleStateKey(threadID string) string {
	return manualThreadTitleStatePrefix + strings.TrimSpace(threadID)
}

func (s *Service) mergeRuntimeThreadMetadata(ctx context.Context, current, fallback model.Thread) model.Thread {
	merged := mergeThreadMetadata(current, fallback)
	threadID := strings.TrimSpace(firstNonEmpty(merged.ID, fallback.ID))
	if threadID == "" {
		return merged
	}
	if title, err := s.store.GetState(ctx, manualThreadTitleStateKey(threadID)); err == nil && strings.TrimSpace(title) != "" {
		merged.Title = title
	}
	return merged
}

func (s *Service) archiveBlockedResponse(ctx context.Context, thread model.Thread) (bool, *DirectResponse) {
	status := currentThreadStatusLabel(thread, nil)
	turnID := strings.TrimSpace(thread.ActiveTurnID)
	if _, snapshot, err := s.loadThreadPanelSnapshot(ctx, thread.ID); err == nil && snapshot != nil {
		status = currentThreadStatusLabel(thread, snapshot)
		turnID = strings.TrimSpace(snapshot.LatestTurnID)
	}
	if status != "处理中" && status != "需要输入" {
		return false, nil
	}
	buttons := [][]model.ButtonSpec{{
		s.callbackButton(ctx, "查看当前进度", "show_thread", thread.ID, turnID, "", nil),
	}}
	return true, &DirectResponse{
		Text:     "「" + threadSelectionTitle(thread) + "」仍在" + status + "，暂时不能归档。\n\n请先完成或停止当前任务。",
		Buttons:  buttons,
		ThreadID: thread.ID,
		TurnID:   turnID,
	}
}

func (s *Service) refreshRenamedThreadCard(ctx context.Context, target model.ObserverTarget, thread model.Thread) {
	panel, err := s.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil || panel == nil || panel.SummaryMessageID == 0 {
		return
	}
	_, snapshot, err := s.loadThreadPanelSnapshot(ctx, thread.ID)
	if err != nil || snapshot == nil {
		return
	}
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return
	}
	var message model.RenderedMessage
	var buttons [][]model.ButtonSpec
	var cardHash string
	if isTerminalStatus(snapshot.LatestTurnStatus) && strings.TrimSpace(snapshot.LatestFinalText) != "" {
		message, buttons, cardHash = s.renderFinalCard(ctx, panel.ID, thread, snapshot)
		panel.LastFinalCardHash = cardHash
	} else {
		pending, _ := s.store.GetLatestPendingApprovalForThread(ctx, thread.ID)
		message, buttons, cardHash = s.renderSummaryPanel(ctx, thread, snapshot, pendingForSnapshot(pending, snapshot))
	}
	if err := sender.EditRenderedMessage(ctx, target.ChatID, target.TopicID, panel.SummaryMessageID, message, buttons); err != nil {
		s.setError(ctx, fmt.Errorf("refresh renamed thread card: %w", err))
		return
	}
	panel.LastSummaryHash = cardHash
	_ = s.store.UpdateThreadPanelFinalCard(ctx, panel.ID, panel.SummaryMessageID, panel.CurrentTurnID, panel.Status, panel.LastSummaryHash, panel.LastToolHash, panel.LastOutputHash, panel.LastFinalNoticeFP, panel.DetailsViewJSON, panel.LastFinalCardHash)
}
