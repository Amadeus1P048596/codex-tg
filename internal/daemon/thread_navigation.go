package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const (
	sessionInboxStatePrefix = "ui.inbox."
	homeRunningSessionLimit = 5
)

type sessionInboxItem struct {
	ThreadID    string            `json:"thread_id"`
	TurnID      string            `json:"turn_id,omitempty"`
	Title       string            `json:"title"`
	State       notificationState `json:"state"`
	UpdatedAt   int64             `json:"updated_at"`
	Fingerprint string            `json:"fingerprint,omitempty"`
}

type homeRunningSession struct {
	Thread   model.Thread
	TurnID   string
	Duration string
}

func sessionInboxTargetPrefix(chatID, topicID int64) string {
	return sessionInboxStatePrefix + model.ChatKey(chatID, topicID) + "."
}

func sessionInboxStateKey(chatID, topicID int64, threadID string) string {
	return sessionInboxTargetPrefix(chatID, topicID) + strings.TrimSpace(threadID)
}

func (s *Service) saveInboxItem(ctx context.Context, chatID, topicID int64, item sessionInboxItem) error {
	item.ThreadID = strings.TrimSpace(item.ThreadID)
	if item.ThreadID == "" {
		return nil
	}
	key := sessionInboxStateKey(chatID, topicID, item.ThreadID)
	if raw, err := s.store.GetState(ctx, key); err == nil && strings.TrimSpace(raw) != "" {
		var existing sessionInboxItem
		if json.Unmarshal([]byte(raw), &existing) == nil && existing.Fingerprint != "" && existing.Fingerprint == item.Fingerprint {
			item.UpdatedAt = existing.UpdatedAt
			if existing.Title == item.Title && existing.TurnID == item.TurnID && existing.State == item.State {
				return nil
			}
		}
	}
	if item.UpdatedAt == 0 {
		item.UpdatedAt = s.currentTime().Unix()
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return s.store.SetState(ctx, key, string(payload))
}

func (s *Service) clearInboxItem(ctx context.Context, chatID, topicID int64, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	return s.store.DeleteState(ctx, sessionInboxStateKey(chatID, topicID, threadID))
}

func (s *Service) inboxItems(ctx context.Context, chatID, topicID int64) ([]sessionInboxItem, error) {
	state, err := s.store.ListState(ctx)
	if err != nil {
		return nil, err
	}
	prefix := sessionInboxTargetPrefix(chatID, topicID)
	items := make([]sessionInboxItem, 0)
	for key, raw := range state {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		var item sessionInboxItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil || strings.TrimSpace(item.ThreadID) == "" {
			continue
		}
		if thread, getErr := s.store.GetThread(ctx, item.ThreadID); getErr == nil && thread != nil {
			if thread.Archived {
				_ = s.clearInboxItem(ctx, chatID, topicID, item.ThreadID)
				continue
			}
			item.Title = threadSelectionTitle(*thread)
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftPriority := inboxStatePriority(items[i].State)
		rightPriority := inboxStatePriority(items[j].State)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if items[i].UpdatedAt != items[j].UpdatedAt {
			return items[i].UpdatedAt > items[j].UpdatedAt
		}
		return items[i].ThreadID < items[j].ThreadID
	})
	return items, nil
}

func inboxStatePriority(state notificationState) int {
	switch state {
	case notificationNeedsInput:
		return 0
	case notificationFailed:
		return 1
	case notificationCompleted:
		return 2
	case notificationCancelled:
		return 3
	default:
		return 4
	}
}

func (s *Service) homeOverview(ctx context.Context, chatID, topicID int64) (*DirectResponse, error) {
	thread, err := s.currentTelegramThread(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	running, runningTotal, err := s.backgroundRunningSessions(ctx, currentThreadID(thread), homeRunningSessionLimit)
	if err != nil {
		return nil, err
	}
	items, err := s.inboxItems(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}

	lines := []string{"Codex 首页"}
	buttons := make([][]model.ButtonSpec, 0, 3+len(running))
	if thread == nil {
		lines = append(lines, "", "当前没有前台会话。")
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, "切换会话", "home_threads", "", "", "", nil),
			s.callbackButton(ctx, "新建会话", "home_new_menu", "", "", "", nil),
		})
	} else {
		status := currentThreadStatusLabel(*thread, nil)
		duration := ""
		if _, snapshot, snapshotErr := s.loadThreadPanelSnapshot(ctx, thread.ID); snapshotErr == nil && snapshot != nil {
			status = currentThreadStatusLabel(*thread, snapshot)
			if status == "处理中" {
				duration = runTimingValue(snapshot, s.currentTime())
			}
		}
		statusLine := status
		if duration != "" {
			statusLine += " · " + duration
		}
		lines = append(lines, "", s.visualMarker(ctx, thread.ID)+" 当前会话", threadSelectionTitle(*thread), statusLine)
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, "查看当前进度", "home_show_current", thread.ID, "", "", nil),
			s.callbackButton(ctx, "切换会话", "home_threads", "", "", "", nil),
		})
	}
	if runningTotal > 0 {
		lines = append(lines, "", fmt.Sprintf("后台运行 · %d", runningTotal))
		for _, session := range running {
			statusLine := "处理中"
			if session.Duration != "" {
				statusLine += " · " + session.Duration
			}
			title := threadSelectionTitle(session.Thread)
			lines = append(lines, s.visualMarker(ctx, session.Thread.ID)+" "+title, statusLine)
			buttons = append(buttons, []model.ButtonSpec{
				s.callbackButton(ctx, shortButtonLabel(s.visualMarker(ctx, session.Thread.ID)+" "+title+" · 处理中"), "show_thread", session.Thread.ID, session.TurnID, "", nil),
			})
		}
		if hidden := runningTotal - len(running); hidden > 0 {
			lines = append(lines, fmt.Sprintf("还有 %d 个运行中会话，可在“切换会话”中查看。", hidden))
		}
	}
	if thread != nil {
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, "新建会话", "home_new_menu", "", "", "", nil),
		})
	}
	if len(items) > 0 {
		lines = append(lines, "", fmt.Sprintf("%d 个后台会话需要关注", len(items)))
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, fmt.Sprintf("待处理 %d", len(items)), "home_inbox", "", "", "", nil),
		})
	} else {
		lines = append(lines, "", "没有后台会话需要处理")
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons, ThreadID: currentThreadID(thread)}, nil
}

func (s *Service) backgroundRunningSessions(ctx context.Context, foregroundThreadID string, limit int) ([]homeRunningSession, int, error) {
	threads, err := s.store.ListThreads(ctx, 500, "")
	if err != nil {
		return nil, 0, err
	}
	if limit < 0 {
		limit = 0
	}
	sessions := make([]homeRunningSession, 0, min(limit, len(threads)))
	total := 0
	for _, thread := range threads {
		if thread.Archived || thread.ID == foregroundThreadID || !threadLooksActiveForPolling(thread) {
			continue
		}
		var snapshot *appserver.ThreadReadSnapshot
		if _, loaded, snapshotErr := s.loadThreadPanelSnapshot(ctx, thread.ID); snapshotErr == nil {
			snapshot = loaded
		}
		if currentThreadStatusLabel(thread, snapshot) != "处理中" {
			continue
		}
		total++
		if len(sessions) >= limit {
			continue
		}
		session := homeRunningSession{Thread: thread}
		if snapshot != nil {
			session.TurnID = snapshot.LatestTurnID
			session.Duration = runTimingValue(snapshot, s.currentTime())
		}
		sessions = append(sessions, session)
	}
	return sessions, total, nil
}

func currentThreadID(thread *model.Thread) string {
	if thread == nil {
		return ""
	}
	return thread.ID
}

func (s *Service) inboxOverview(ctx context.Context, chatID, topicID int64) (*DirectResponse, error) {
	items, err := s.inboxItems(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	buttons := make([][]model.ButtonSpec, 0, len(items)+1)
	for _, item := range items {
		title := compactThreadSelectionText(item.Title)
		if title == "" {
			title = "未命名会话"
		}
		label := shortButtonLabel(s.visualMarker(ctx, item.ThreadID) + " " + title + " · " + backgroundThreadStateLabel(item.State))
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, label, "switch_thread", item.ThreadID, item.TurnID, "", nil),
		})
	}
	buttons = append(buttons, []model.ButtonSpec{s.callbackButton(ctx, "返回首页", "home_overview", "", "", "", nil)})
	if len(items) == 0 {
		return &DirectResponse{Text: "待处理\n\n暂无后台会话需要处理。", Buttons: buttons}, nil
	}
	return &DirectResponse{Text: fmt.Sprintf("待处理 · %d\n\n点击会话切换并查看详情。", len(items)), Buttons: buttons}, nil
}

func (s *Service) newSessionMenu(ctx context.Context) *DirectResponse {
	return &DirectResponse{
		Text: "新建会话\n\n请选择会话类型。",
		Buttons: [][]model.ButtonSpec{
			{
				s.callbackButton(ctx, "新建 Chat", "home_new_chat", "", "", "", nil),
				s.callbackButton(ctx, "新建普通会话", "home_new_thread", "", "", "", nil),
			},
			{s.callbackButton(ctx, "返回首页", "home_overview", "", "", "", nil)},
		},
	}
}

func (s *Service) editNavigationResponse(ctx context.Context, chatID, topicID, messageID int64, response *DirectResponse, callbackText string) (*DirectResponse, error) {
	if response == nil {
		return &DirectResponse{CallbackText: callbackText}, nil
	}
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil || messageID == 0 {
		response.CallbackText = callbackText
		return response, nil
	}
	if err := sender.EditMessage(ctx, chatID, topicID, messageID, response.Text, response.Buttons); err != nil {
		response.CallbackText = callbackText
		return response, nil
	}
	return &DirectResponse{CallbackText: callbackText}, nil
}

func (s *Service) showCurrentFromHome(ctx context.Context, chatID, topicID, messageID int64) (*DirectResponse, error) {
	thread, err := s.currentTelegramThread(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return s.editNavigationResponse(ctx, chatID, topicID, messageID, &DirectResponse{Text: "当前没有活动会话。"}, "当前没有活动会话。")
	}
	response, err := s.showThread(ctx, chatID, topicID, thread.ID, false)
	if err != nil || response == nil || strings.TrimSpace(response.ThreadID) == "" {
		return response, err
	}
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender != nil && messageID != 0 {
		if err := sender.DeleteMessage(ctx, chatID, topicID, messageID); err != nil {
			s.setError(ctx, fmt.Errorf("delete home card after showing current session: %w", err))
		}
	}
	return &DirectResponse{CallbackText: "已显示当前会话。", ThreadID: thread.ID}, nil
}
