package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

type recordedMessage struct {
	chatID    int64
	topicID   int64
	messageID int64
	text      string
	entities  []model.MessageEntity
	parseMode string
	plainText string
	buttons   [][]model.ButtonSpec
	options   model.SendOptions
}

type recordedDocument struct {
	chatID   int64
	topicID  int64
	fileName string
	filePath string
	data     []byte
	caption  string
	options  model.SendOptions
}

type recordingSender struct {
	messages  []recordedMessage
	documents []recordedDocument
	edits     []recordedMessage
	deletes   []recordedMessage
	editErr   error
}

func (s *recordingSender) SendMessage(ctx context.Context, chatID, topicID int64, text string, buttons [][]model.ButtonSpec, options model.SendOptions) (int64, error) {
	messageID := int64(len(s.messages) + 1)
	s.messages = append(s.messages, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: text, buttons: buttons, options: options})
	return messageID, nil
}

func (s *recordingSender) SendRenderedMessages(ctx context.Context, chatID, topicID int64, messages []model.RenderedMessage, buttons [][]model.ButtonSpec, options model.SendOptions) ([]int64, error) {
	ids := make([]int64, 0, len(messages))
	for _, message := range messages {
		messageID := int64(len(s.messages) + 1)
		s.messages = append(s.messages, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: message.Text, entities: message.Entities, parseMode: message.ParseMode, plainText: message.PlainText, buttons: buttons, options: options})
		ids = append(ids, messageID)
	}
	return ids, nil
}

func (s *recordingSender) EditMessage(ctx context.Context, chatID, topicID, messageID int64, text string, buttons [][]model.ButtonSpec) error {
	if s.editErr != nil {
		return s.editErr
	}
	s.edits = append(s.edits, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: text, buttons: buttons})
	return nil
}

func (s *recordingSender) EditRenderedMessage(ctx context.Context, chatID, topicID, messageID int64, rendered model.RenderedMessage, buttons [][]model.ButtonSpec) error {
	if s.editErr != nil {
		return s.editErr
	}
	s.edits = append(s.edits, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: rendered.Text, entities: rendered.Entities, parseMode: rendered.ParseMode, plainText: rendered.PlainText, buttons: buttons})
	return nil
}

func (s *recordingSender) DeleteMessage(ctx context.Context, chatID, topicID, messageID int64) error {
	s.deletes = append(s.deletes, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID})
	return nil
}

func (s *recordingSender) SendDocumentData(ctx context.Context, chatID, topicID int64, fileName string, data []byte, caption string, options model.SendOptions) (int64, error) {
	s.documents = append(s.documents, recordedDocument{
		chatID:   chatID,
		topicID:  topicID,
		fileName: fileName,
		data:     append([]byte(nil), data...),
		caption:  caption,
		options:  options,
	})
	return int64(len(s.documents)), nil
}

func hasRecordedEntity(entities []model.MessageEntity, entityType, language string) bool {
	for _, entity := range entities {
		if entity.Type != entityType {
			continue
		}
		if language == "" || entity.Language == language {
			return true
		}
	}
	return false
}

func hasHeaderKind(text, kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "final":
		return strings.Contains(text, "<b>已完成</b>") || strings.Contains(text, "<b>失败</b>") || strings.Contains(text, "<b>已取消</b>")
	case "commentary":
		return strings.Contains(text, "<b>处理中</b>") || strings.Contains(text, "<b>需要输入</b>")
	case "plan":
		return strings.Contains(text, "<b>Codex</b> · <b>需要输入</b>")
	case "user":
		return strings.Contains(text, "<b>Codex</b> · <b>请求</b>")
	}
	headerLines := strings.SplitN(text, "\n", 4)
	header := strings.Join(headerLines[:minInt(3, len(headerLines))], "\n")
	return strings.Contains(header, "["+kind+"]") || strings.Contains(header, visualRole(kind))
}

func finalMessages(messages []recordedMessage) []recordedMessage {
	out := make([]recordedMessage, 0, len(messages))
	for _, message := range messages {
		if hasHeaderKind(message.text, "Final") {
			out = append(out, message)
		}
	}
	return out
}

func lastFinalMessage(t *testing.T, messages []recordedMessage) recordedMessage {
	t.Helper()
	finals := finalMessages(messages)
	if len(finals) == 0 {
		t.Fatalf("final message not found in %#v", messages)
	}
	return finals[len(finals)-1]
}

func hasThreadChip(text, threadID string) bool {
	headerLines := strings.SplitN(text, "\n", 4)
	header := strings.Join(headerLines[:minInt(3, len(headerLines))], "\n")
	shortID := visualShortID(threadID)
	return strings.Contains(header, "[T:"+shortID+"]") || strings.Contains(header, "T:"+shortID)
}

func hasRecordedEntityForText(message recordedMessage, entityType, text string) bool {
	index := strings.Index(message.text, text)
	if index < 0 {
		return false
	}
	wantOffset := visualUTF16Len(message.text[:index])
	wantLength := visualUTF16Len(text)
	for _, entity := range message.entities {
		if entity.Type == entityType && entity.Offset == wantOffset && entity.Length == wantLength {
			return true
		}
	}
	return false
}

func TestUserAndFinalCardsEmphasizeIdentityStatusAndTiming(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	thread := model.Thread{
		ID:          "thread-card-hierarchy",
		Title:       "Investigate sync status",
		ProjectName: "Codex",
		Status:      "idle",
	}
	snapshot := &appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-card-hierarchy",
		LatestTurnStatus:      "completed",
		LatestTurnStartedAt:   "2026-08-20T10:00:00Z",
		LatestTurnUpdatedAt:   "2026-08-20T10:01:19Z",
		LatestUserMessageText: "Check the task.",
		LatestFinalText:       "The task completed.",
	}

	user := service.renderUserRequestNoticeCard(ctx, thread, snapshot)[0]
	if user.ParseMode != "HTML" || !strings.Contains(user.Text, "<b>Codex</b> · <b>请求</b>") {
		t.Fatalf("user card = %#v, want HTML request hierarchy", user)
	}
	if !strings.Contains(user.Text, "<code>T:thread · R:turn</code>") {
		t.Fatalf("user card = %#v, want collapsed code metadata", user)
	}

	final, _, _ := service.renderFinalCard(ctx, 1, thread, snapshot)
	marker := service.visualMarker(ctx, thread.ID)
	for _, want := range []string{marker + " <b>Investigate sync status</b>\n<b>Codex</b> · <b>已完成</b> · 1m 19s", "<code>T:thread · R:turn</code>"} {
		if !strings.Contains(final.Text, want) {
			t.Fatalf("final card = %#v, want %q", final, want)
		}
	}
	if strings.Index(final.Text, "1m 19s") > strings.Index(final.Text, snapshot.LatestFinalText) {
		t.Fatalf("final card text = %q, want timing in header before response body", final.Text)
	}
}

func TestSyncThreadPanelDoesNotDuplicateFinalAnswerOnRepeatedSync(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-1",
		Title:       "Observer smoke",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-1",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-1",
		LatestFinalText:  "All work complete.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "stable")
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "stable")

	finalCount := len(finalMessages(sender.messages))
	if finalCount != 1 {
		t.Fatalf("final message count = %d, want 1; messages=%#v", finalCount, sender.messages)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("message count = %d, want one Final card on first sync only; messages=%#v", len(sender.messages), sender.messages)
	}
	if len(sender.documents) != 0 {
		t.Fatalf("documents = %#v, want no tool documents for a completed turn without tool output", sender.documents)
	}
	if len(sender.deletes) != 0 {
		t.Fatalf("deletes = %#v, want stable single-card lifecycle", sender.deletes)
	}

	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil {
		t.Fatal("GetCurrentThreadPanel returned nil")
	}
	if panel.SourceMode != "stable" {
		t.Fatalf("panel SourceMode = %q, want stable", panel.SourceMode)
	}

	if panel.LastFinalNoticeFP != "final-fp-1" {
		t.Fatalf("panel LastFinalNoticeFP = %q, want final-fp-1", panel.LastFinalNoticeFP)
	}
}

func TestSyncThreadPanelFormatsFinalAnswerMarkdownWithEntities(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-md-final",
		Title:       "Markdown final",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-md-final",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-md-fp",
		LatestFinalText:  "Run `rg`:\n\n```bash\nrg -n 'Authorization' stellar_ws.txt\n```",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "explicit")

	final := lastFinalMessage(t, sender.messages)
	if strings.Contains(final.text, "```") {
		t.Fatalf("final message still contains raw markdown fence: %q", final.text)
	}
	if final.parseMode != "HTML" || !strings.Contains(final.text, "<code>rg</code>") {
		t.Fatalf("final = %#v, want HTML inline code", final)
	}
	if !strings.Contains(final.text, `<pre><code class="language-bash">`) {
		t.Fatalf("final = %#v, want bash pre block", final)
	}
}

func TestFinalTransitionEditsWorkingCardInPlaceWithoutDeletes(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-final-delete-run-notice",
		Title:       "Final cleanup",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	initial := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-final-cleanup",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-final-cleanup",
		LatestUserMessageText: "Run cleanup smoke.",
		LatestUserMessageFP:   "user-final-cleanup-fp",
		LatestToolID:          "tool-final-cleanup",
		LatestToolLabel:       "Write-Output cleanup",
		LatestToolStatus:      "running",
		LatestToolFP:          "tool-final-cleanup-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, initial); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("messages = %#v, want one Working card", sender.messages)
	}
	summaryID := sender.messages[0].messageID

	completed := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-final-cleanup",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-final-cleanup",
		LatestUserMessageText: "Run cleanup smoke.",
		LatestUserMessageFP:   "user-final-cleanup-fp",
		LatestFinalFP:         "final-cleanup-fp",
		LatestFinalText:       "Cleanup done.",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{{
			ID:    "agent-final-cleanup",
			Phase: "commentary",
			Text:  "This completed commentary belongs in Details only.",
			FP:    "agent-final-cleanup-fp",
		}},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, completed); err != nil {
		t.Fatalf("UpsertSnapshot(completed) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.deletes) != 0 {
		t.Fatalf("deletes = %#v, want no delete/re-send transition", sender.deletes)
	}
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %#v, want one in-place final edit", sender.edits)
	}
	final := sender.edits[0]
	if final.messageID != summaryID || !hasHeaderKind(final.text, "Final") {
		t.Fatalf("final = %#v, want Completed edit on Working message %d", final, summaryID)
	}
	finalText := final.text
	if strings.Contains(finalText, "[commentary]") || strings.Contains(finalText, "This completed commentary belongs in Details only.") {
		t.Fatalf("final text = %q, want final answer without commentary transcript", finalText)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID != summaryID || panel.LastFinalNoticeFP != "final-cleanup-fp" {
		t.Fatalf("panel = %#v, want SummaryMessageID retained at %d with final fp", panel, summaryID)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, final.messageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute(final) failed: %v", err)
	}
	if route == nil || route.ThreadID != thread.ID || route.TurnID != "turn-final-cleanup" || route.EventID != "final-cleanup-fp" {
		t.Fatalf("final route = %#v, want thread/turn/final route", route)
	}
}

func TestSummaryPanelFormatsCommentaryMarkdownWithoutFinalLabel(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-md-summary",
		Title:       "Markdown summary",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-md-summary",
		LatestTurnStatus: "inProgress",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{{
			ID:    "agent-1",
			Phase: "commentary",
			Text:  "Checking `node`:\n\n```powershell\nnode -v\n```",
			FP:    "agent-1-fp",
		}},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "explicit")

	if len(sender.messages) == 0 {
		t.Fatal("no summary message was sent")
	}
	summary := sender.messages[0]
	if !hasHeaderKind(summary.text, "commentary") || strings.Contains(summary.text, "Checking `node`") || strings.Contains(summary.text, "node -v") {
		t.Fatalf("summary = %#v, want stable Working card without raw commentary events", summary)
	}
}

func TestSummaryPanelDisplaysAgentMessagesChronologically(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "thread-order",
		Title:       "Summary order",
		ProjectName: "Codex",
	}
	snapshot := &appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-order",
		LatestTurnStatus: "inProgress",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{
			{ID: "agent-3", Phase: "commentary", Text: "THIRD newest"},
			{ID: "agent-2", Phase: "commentary", Text: "SECOND middle"},
			{ID: "agent-1", Phase: "commentary", Text: "FIRST oldest"},
		},
	}

	service := newTestService(t)
	messages := service.renderSummaryPanelMarkdown(context.Background(), thread, snapshot, snapshot.LatestAgentMessageEntries, nil)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	text := messages[0].Text
	if !strings.Contains(text, "<b>处理中</b>") || !strings.Contains(text, "正在处理请求") {
		t.Fatalf("summary text = %q, want stable Working state", text)
	}
	for _, raw := range []string{"FIRST oldest", "SECOND middle", "THIRD newest", "[commentary]"} {
		if strings.Contains(text, raw) {
			t.Fatalf("summary text leaked raw commentary %q: %q", raw, text)
		}
	}
}

func TestSummaryPanelRemovesNilLiteralBeforeRendering(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "thread-nil-summary",
		Title:       "Nil summary",
		ProjectName: "Codex",
	}
	snapshot := &appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-nil-summary",
		LatestTurnStatus: "<nil>",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{
			{ID: "agent-nil", Phase: "<nil>", Text: "Before <nil> after"},
		},
	}

	service := newTestService(t)
	messages := service.renderSummaryPanelMarkdown(context.Background(), thread, snapshot, snapshot.LatestAgentMessageEntries, nil)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if strings.Contains(messages[0].Text, "<nil>") {
		t.Fatalf("summary text leaked nil literal: %q", messages[0].Text)
	}
	if strings.Contains(messages[0].Text, "Before") || !strings.Contains(messages[0].Text, "正在处理请求") {
		t.Fatalf("summary text = %q, want aggregator-owned progress copy", messages[0].Text)
	}
}

func TestSyncThreadPanelDoesNotUseDocumentDeliveryForToolOutput(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-tool",
		Title:       "Tool output smoke",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	snapshot := appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-tool",
		LatestTurnStatus: "completed",
		LatestToolID:     "tool-1",
		LatestToolKind:   "commandExecution",
		LatestToolLabel:  "pwsh -Command node -v",
		LatestToolStatus: "completed",
		LatestToolOutput: "v22.22.2\n",
		LatestToolFP:     "tool-fp-1",
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	compact := appserver.CompactSnapshot(nil, snapshot, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, compact); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}, thread.ID, false, "explicit")

	if len(sender.documents) != 0 {
		t.Fatalf("documents = %#v, want no SendDocument path for tool output", sender.documents)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("message count = %d, want one aggregated status card", len(sender.messages))
	}
	if got := sender.messages[0].text; strings.Contains(got, "[Tool]") || strings.Contains(got, "[Output]") || strings.Contains(got, "v22.22.2") || !strings.Contains(got, "1 次操作") {
		t.Fatalf("status card = %q, want operation count without raw tool/output cards", got)
	}
}

func TestGlobalObserverSyncSendsUserRequestNoticeOnceBeforeTrio(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-user-notice",
		Title:       "GUI prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-user-notice",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-item-1",
		LatestUserMessageText: "Check `node -v` from GUI.",
		LatestUserMessageFP:   "user-fp-1",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("messages = %#v, want one Working card across repeated observer sync", sender.messages)
	}
	if strings.Contains(sender.messages[0].text, "New run:") || hasHeaderKind(sender.messages[0].text, "User") || strings.Contains(sender.messages[0].text, "Check `node -v`") {
		t.Fatalf("status card leaked separate run/user event UI: %q", sender.messages[0].text)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID != sender.messages[0].messageID || panel.RunNoticeMessageID != 0 || panel.UserMessageID != 0 {
		t.Fatalf("panel notice state = %#v, want only summary message %d", panel, sender.messages[0].messageID)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, sender.messages[0].messageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute failed: %v", err)
	}
	if route == nil || route.ThreadID != thread.ID || route.TurnID != "turn-user-notice" {
		t.Fatalf("status route = %#v, want thread/turn route", route)
	}
}

func TestGlobalObserverSyncCreatesRunNoticeAndUserPlaceholderBeforeTrioWithoutUserPrompt(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-orientation-no-user",
		Title:       "GUI tool first",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-tool-first",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-first",
		LatestToolLabel:  "Start-Sleep -Seconds 900",
		LatestToolStatus: "running",
		LatestToolFP:     "tool-first-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 {
		t.Fatalf("messages = %#v, want one Working card", sender.messages)
	}
	if got := sender.messages[0].text; !strings.Contains(got, "<b>进度</b>") || !strings.Contains(got, "Start-Sleep -Seconds 900") || strings.Contains(got, "User prompt was not available") {
		t.Fatalf("status card = %q, want current command without placeholder/debug cards", got)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID != sender.messages[0].messageID || panel.RunNoticeMessageID != 0 || panel.UserMessageID != 0 {
		t.Fatalf("panel = %#v, want one summary message only", panel)
	}
}

func TestLateUserPromptEditsExistingUserPlaceholder(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-late-user-orientation",
		Title:       "Late user",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	initial := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-late-user",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-late-user",
		LatestToolLabel:  "Start-Sleep -Seconds 900",
		LatestToolStatus: "running",
		LatestToolFP:     "tool-late-user-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, initial); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("initial messages = %#v, want one Working card", sender.messages)
	}
	statusMessageID := sender.messages[0].messageID

	late := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-late-user",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-late",
		LatestUserMessageText: "Use `mtkachenko2` config.",
		LatestUserMessageFP:   "user-late-fp",
		LatestToolID:          "tool-late-user",
		LatestToolLabel:       "Start-Sleep -Seconds 900",
		LatestToolStatus:      "running",
		LatestToolFP:          "tool-late-user-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, late); err != nil {
		t.Fatalf("UpsertSnapshot(late) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 {
		t.Fatalf("messages = %#v, want no late appended user message", sender.messages)
	}
	for _, edit := range sender.edits {
		if strings.Contains(edit.text, "mtkachenko2") || hasHeaderKind(edit.text, "User") {
			t.Fatalf("edits = %#v, want raw user event kept out of the status card", sender.edits)
		}
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID != statusMessageID || panel.UserMessageID != 0 {
		t.Fatalf("panel = %#v, want one stable status message", panel)
	}

	editCount := len(sender.edits)
	statusOnly := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-late-user",
		LatestTurnStatus:      "interrupted",
		LatestUserMessageID:   "user-late",
		LatestUserMessageText: "Use `mtkachenko2` config.",
		LatestUserMessageFP:   "user-late-fp",
		LatestToolID:          "tool-late-user",
		LatestToolLabel:       "Start-Sleep -Seconds 900",
		LatestToolStatus:      "running",
		LatestToolFP:          "tool-late-user-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, statusOnly); err != nil {
		t.Fatalf("UpsertSnapshot(statusOnly) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.edits) != editCount+1 || sender.edits[len(sender.edits)-1].messageID != statusMessageID || !strings.Contains(sender.edits[len(sender.edits)-1].text, "<b>已取消</b>") {
		t.Fatalf("edits = %#v, want terminal status edited in place", sender.edits)
	}
}

func TestTelegramInputSyncDoesNotDuplicateUserRequestNotice(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-telegram-input",
		Title:       "Telegram prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-telegram-input",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-telegram",
		LatestUserMessageText: "This was already sent in Telegram.",
		LatestUserMessageFP:   "user-telegram-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)

	if len(sender.messages) != 1 {
		t.Fatalf("message count = %d, want one Working card without a user duplicate; messages=%#v", len(sender.messages), sender.messages)
	}
	if !strings.Contains(sender.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("message = %q, want Working card", sender.messages[0].text)
	}
	for _, message := range sender.messages {
		if hasHeaderKind(message.text, "User") {
			t.Fatalf("unexpected user notice for Telegram-originated input: %#v", sender.messages)
		}
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SourceMode != model.PanelSourceTelegramInput || panel.SummaryMessageID != sender.messages[0].messageID || panel.RunNoticeMessageID != 0 || panel.LastUserNoticeFP != "" {
		t.Fatalf("panel = %#v, want telegram_input with one summary card", panel)
	}
}

func TestGlobalObserverDoesNotRecreateTelegramOriginPanelOnEditFailure(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-telegram-origin-no-global-duplicate",
		Title:       "Telegram prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.markTelegramOriginTurn(ctx, thread.ID, "turn-telegram-origin-no-global-duplicate"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-telegram-origin-no-global-duplicate",
		LatestTurnStatus: "inProgress",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)
	if len(sender.messages) != 1 {
		t.Fatalf("initial message count = %d, want one Working card; messages=%#v", len(sender.messages), sender.messages)
	}
	panelBefore, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel(before) failed: %v", err)
	}
	if panelBefore == nil || panelBefore.SourceMode != model.PanelSourceTelegramInput {
		t.Fatalf("panel before = %#v, want telegram_input", panelBefore)
	}

	nextSnapshot := appserver.CompactSnapshot(&snapshot, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-telegram-origin-no-global-duplicate",
		LatestTurnStatus: "inProgress",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{
			{ID: "agent-1", Phase: model.DetailItemCommentary, Text: "Working.", FP: "agent-1-fp"},
		},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, nextSnapshot); err != nil {
		t.Fatalf("UpsertSnapshot(next) failed: %v", err)
	}
	sender.editErr = errors.New("forced edit failure")
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 {
		t.Fatalf("message count after global sync = %d, want no duplicate status card; messages=%#v", len(sender.messages), sender.messages)
	}
	panelAfter, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel(after) failed: %v", err)
	}
	if panelAfter == nil || panelAfter.ID != panelBefore.ID || panelAfter.SourceMode != model.PanelSourceTelegramInput {
		t.Fatalf("panel after = %#v, want original telegram_input panel %#v", panelAfter, panelBefore)
	}
}

func TestMarkedTelegramOriginTurnDoesNotDuplicateUserRequestNoticeOnObserverResync(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-marked-telegram-input",
		Title:       "Marked Telegram prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.markTelegramOriginTurn(ctx, thread.ID, "turn-marked-telegram"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       model.PanelSourceGlobalObserver,
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "turn-old",
		Status:           "completed",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-marked-telegram",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-marked-telegram",
		LatestUserMessageText: "This was sent from Telegram and later re-polled.",
		LatestUserMessageFP:   "user-marked-telegram-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	for _, message := range sender.messages {
		if hasHeaderKind(message.text, "User") {
			t.Fatalf("unexpected user notice for marked Telegram-origin turn: %#v", sender.messages)
		}
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.LastUserNoticeFP != "" {
		t.Fatalf("panel = %#v, want no user notice fp for marked Telegram-origin turn", panel)
	}
}

func TestTelegramInputSyncAdoptsObserverPanelForSameTurn(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-adopt-observer-panel",
		Title:       "Telegram race",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       model.PanelSourceGlobalObserver,
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "turn-telegram-race",
		Status:           "inProgress",
		ArchiveEnabled:   true,
		LastSummaryHash:  "old-summary",
		LastToolHash:     "old-tool",
		LastOutputHash:   "old-output",
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	if err := service.markTelegramOriginTurn(ctx, thread.ID, "turn-telegram-race"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-telegram-race",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-telegram-race",
		LatestUserMessageText: "Проверка, ответь: Test",
		LatestUserMessageFP:   "user-telegram-race-fp",
		LatestFinalText:       "Test",
		LatestFinalFP:         "final-telegram-race-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)

	if len(sender.messages) != 1 || len(sender.edits) != 1 {
		t.Fatalf("messages=%#v edits=%#v, want one in-place Final edit and one terminal notice", sender.messages, sender.edits)
	}
	if sender.messages[0].options.Silent || !strings.Contains(sender.messages[0].text, "<b>Telegram race</b> 已完成") {
		t.Fatalf("terminal notice=%#v, want audible completion", sender.messages[0])
	}
	final := sender.edits[0]
	if final.messageID != 101 || !hasHeaderKind(final.text, "Final") {
		t.Fatalf("final = %#v, want Final on observer message 101", final)
	}
	if len(sender.deletes) != 0 {
		t.Fatalf("deletes = %#v, want no delete/re-send transition", sender.deletes)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID != 101 || panel.SourceMode != model.PanelSourceTelegramInput || panel.LastFinalNoticeFP != "final-telegram-race-fp" {
		t.Fatalf("panel = %#v, want adopted panel retained at message 101 with final fp", panel)
	}
}

func TestStablePanelSendsNewUserNoticeForNewForeignTurnWithoutNewTrio(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	if err := service.store.SetState(ctx, "ui.panel_mode", model.PanelModeStable); err != nil {
		t.Fatalf("SetState(ui.panel_mode) failed: %v", err)
	}

	thread := model.Thread{
		ID:          "thread-stable-user-notice",
		Title:       "Stable GUI prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       model.PanelSourceGlobalObserver,
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "turn-old",
		Status:           "completed",
		ArchiveEnabled:   true,
		LastUserNoticeFP: "user-old-fp",
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-new",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-new",
		LatestUserMessageText: "New GUI prompt.",
		LatestUserMessageFP:   "user-new-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 0 {
		t.Fatalf("messages = %#v, want no late [User] append below legacy stable trio", sender.messages)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.UserMessageID != 0 || panel.LastUserNoticeFP != "user-old-fp" {
		t.Fatalf("panel user notice state = %#v, want legacy state unchanged without late append", panel)
	}
}

func TestSyncThreadPanelToTargetSkipsInitialTerminalGlobalObserverReplay(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-terminal-global",
		Title:       "Completed elsewhere",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Add(-10 * time.Minute).Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-terminal",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-terminal",
		LatestFinalText:  "Already done.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 0 {
		t.Fatalf("message count = %d, want 0 for terminal global observer replay; messages=%#v", len(sender.messages), sender.messages)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel != nil {
		t.Fatalf("expected no panel for initial terminal global observer replay, got %#v", panel)
	}
}

func TestSyncThreadPanelToTargetCreatesPanelForRecentTerminalGlobalObserverChange(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-recent-terminal-global",
		Title:       "Completed just now",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-recent-terminal",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-recent-terminal",
		LatestFinalText:  "Fresh completion.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 1 {
		t.Fatalf("message count = %d, want one Final card for recent terminal observer change; messages=%#v", len(sender.messages), sender.messages)
	}
	final := lastFinalMessage(t, sender.messages)
	if final.options.Silent {
		t.Fatalf("final = %#v, want audible Final message", final)
	}
	if len(sender.deletes) != 0 {
		t.Fatalf("deletes = %#v, want no synthetic working-card cleanup", sender.deletes)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil {
		t.Fatal("expected panel for recent terminal observer change")
	}
}

func TestTerminalObserverPanelWithRunNoticeCollapsesWhenFinalAppears(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-terminal-run-notice-final",
		Title:       "Terminal with prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	initial := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-terminal-run-notice",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-terminal-run-notice",
		LatestUserMessageText: "Finish from GUI.",
		LatestUserMessageFP:   "user-terminal-run-notice-fp",
		LatestToolID:          "tool-terminal-run-notice",
		LatestToolLabel:       "go test ./...",
		LatestToolStatus:      "completed",
		LatestToolOutput:      "ok\n",
		LatestToolFP:          "tool-terminal-run-notice-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, initial); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 1 {
		t.Fatalf("initial messages = %#v, want one terminal status card", sender.messages)
	}
	summaryID := sender.messages[0].messageID

	finalSnapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-terminal-run-notice",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-terminal-run-notice",
		LatestUserMessageText: "Finish from GUI.",
		LatestUserMessageFP:   "user-terminal-run-notice-fp",
		LatestFinalFP:         "final-terminal-run-notice-fp",
		LatestFinalText:       "Done from final answer.",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{{
			ID:    "agent-terminal-run-notice",
			Phase: "commentary",
			Text:  "Completed commentary should stay in Details.",
			FP:    "agent-terminal-run-notice-fp",
		}},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, finalSnapshot); err != nil {
		t.Fatalf("UpsertSnapshot(final) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.deletes) != 0 {
		t.Fatalf("deletes = %#v, want no delete/re-send transition", sender.deletes)
	}
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %#v, want one in-place Final edit", sender.edits)
	}
	finalEdit := sender.edits[0]
	if finalEdit.messageID != summaryID || !hasHeaderKind(finalEdit.text, "Final") {
		t.Fatalf("final edit = %#v, want Final on message %d", finalEdit, summaryID)
	}
	if strings.Contains(finalEdit.text, "[commentary]") || strings.Contains(finalEdit.text, "Completed commentary should stay in Details.") {
		t.Fatalf("final edit = %q, want final answer only", finalEdit.text)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.LastFinalNoticeFP != "final-terminal-run-notice-fp" || panel.SummaryMessageID != summaryID {
		t.Fatalf("panel = %#v, want final fingerprint recorded", panel)
	}
}

func TestTerminalSyncDoesNotRewriteRunNotice(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-terminal-run-notice-no-edit",
		Title:       "Terminal no run edit",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:             target.ChatID,
		TopicID:            target.TopicID,
		ProjectName:        thread.ProjectName,
		ThreadID:           thread.ID,
		SourceMode:         model.PanelSourceGlobalObserver,
		SummaryMessageID:   102,
		ToolMessageID:      103,
		OutputMessageID:    104,
		RunNoticeMessageID: 101,
		LastRunNoticeFP:    "legacy-run-notice-with-status",
		CurrentTurnID:      "turn-terminal-no-run-edit",
		Status:             "inProgress",
		ArchiveEnabled:     true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-terminal-no-run-edit",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-terminal-no-run-edit",
		LatestUserMessageText: "Finish without final yet.",
		LatestUserMessageFP:   "user-terminal-no-run-edit-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	for _, edit := range sender.edits {
		if edit.messageID == 101 {
			t.Fatalf("edits = %#v, want no terminal run-notice rewrite", sender.edits)
		}
	}
}

func TestRenderToolPanelUnwrapsQuotedPowerShellCommand(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	text, _ := service.renderToolPanel(context.Background(), model.Thread{
		ID:          "thread-tool-pwsh",
		Title:       "Find Swagger",
		ProjectName: "Codex",
	}, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-tool-pwsh",
		LatestToolLabel:  `"C:\Program Files\PowerShell\7\pwsh.exe" -Command "rg -n 'session/33655d2a' 'C:\Users\you\Downloads\stellar_ws.txt'"`,
		LatestToolStatus: "completed",
	})

	if !strings.Contains(text, "[Shell:pwsh.exe (PowerShell)]") {
		t.Fatalf("rendered tool = %q, want shell metadata line", text)
	}
	if strings.Contains(text, "C:\\Program Files\\PowerShell") {
		t.Fatalf("rendered tool = %q, want wrapper shell path omitted", text)
	}
	if !strings.Contains(text, `<pre><code class="language-powershell">rg -n &#39;session/33655d2a&#39; &#39;C:\Users\you\Downloads\stellar_ws.txt&#39;</code></pre>`) {
		t.Fatalf("rendered tool = %q, want inner command in powershell code block", text)
	}
}

func TestRenderToolPanelMarksUnknownShell(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	text, _ := service.renderToolPanel(context.Background(), model.Thread{
		ID:          "thread-tool-unknown",
		Title:       "Unknown shell",
		ProjectName: "Codex",
	}, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-tool-unknown",
		LatestToolLabel:  `hui.exe -Command "echo 1"`,
		LatestToolStatus: "completed",
	})

	if !strings.Contains(text, "[Shell:hui.exe (⚠️UNKNOWN SHELL⚠️)]") {
		t.Fatalf("rendered tool = %q, want unknown shell marker", text)
	}
	if !strings.Contains(text, "<pre><code>echo 1</code></pre>") {
		t.Fatalf("rendered tool = %q, want command in generic code block", text)
	}
}

func TestRenderToolPanelShowsLastCompletedToolInsteadOfRunningTool(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	now := time.Date(2026, time.May, 1, 23, 19, 21, 0, time.UTC)
	text, _ := service.renderToolPanelAt(context.Background(), model.Thread{
		ID:          "thread-tool-running",
		Title:       "Long command",
		ProjectName: "Codex",
	}, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-tool-running",
		LatestTurnStatus: "inProgress",
		LatestToolLabel:  `sleep 20; printf 'slow-command-done\n'`,
		LatestToolStatus: "running",
		DetailItems: []model.DetailItem{
			{
				ID:     "tool-prev",
				Kind:   model.DetailItemTool,
				Label:  `printf 'previous\n'`,
				Status: "completed",
			},
			{
				ID:     "tool-prev:output",
				Kind:   model.DetailItemOutput,
				Output: "previous\n",
			},
		},
	}, now)

	if !strings.Contains(text, "Last completed tool:") {
		t.Fatalf("rendered tool = %q, want last completed label", text)
	}
	if !strings.Contains(text, "previous") || !strings.Contains(text, "Status: completed") {
		t.Fatalf("rendered tool = %q, want previous completed command", text)
	}
	if strings.Contains(text, "slow-command-done") || strings.Contains(text, "Status: running") {
		t.Fatalf("rendered tool = %q, want running command hidden from Tool panel", text)
	}
	if strings.Contains(text, "Running for:") || strings.Contains(text, "Run active for:") {
		t.Fatalf("rendered tool = %q, want run timing outside Tool panel", text)
	}
}

func TestRenderToolPanelShowsTelegramOriginCurrentTool(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	thread := model.Thread{
		ID:          "thread-telegram-current-tool",
		Title:       "Long command",
		ProjectName: "Codex",
	}
	turnID := "turn-telegram-current-tool"
	if err := service.markTelegramOriginTurn(ctx, thread.ID, turnID); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	text, _ := service.renderToolPanelAt(ctx, thread, &appserver.ThreadReadSnapshot{
		LatestTurnID:          turnID,
		LatestTurnStatus:      "inProgress",
		LatestToolLabel:       `sleep 20; printf 'slow-command-done\n'`,
		LatestToolStatus:      "running",
		LatestToolLiveCurrent: true,
		DetailItems: []model.DetailItem{
			{
				ID:     "tool-prev",
				Kind:   model.DetailItemTool,
				Label:  `printf 'previous\n'`,
				Status: "completed",
			},
			{
				ID:     "tool-prev:output",
				Kind:   model.DetailItemOutput,
				Output: "previous\n",
			},
		},
	}, time.Date(2026, time.May, 1, 23, 19, 21, 0, time.UTC))

	if !strings.Contains(text, "Current tool:") {
		t.Fatalf("rendered tool = %q, want current tool heading", text)
	}
	if !strings.Contains(text, "slow-command-done") || !strings.Contains(text, "Status: running") {
		t.Fatalf("rendered tool = %q, want running current command", text)
	}
	if strings.Contains(text, "Last completed tool:") || strings.Contains(text, "previous") {
		t.Fatalf("rendered tool = %q, want current command to take precedence", text)
	}
}

func TestRenderToolPanelKeepsForeignRunningToolHidden(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	thread := model.Thread{
		ID:          "thread-foreign-running-tool",
		Title:       "Foreign command",
		ProjectName: "Codex",
	}
	text, _ := service.renderToolPanelAt(ctx, thread, &appserver.ThreadReadSnapshot{
		LatestTurnID:          "turn-foreign-running-tool",
		LatestTurnStatus:      "inProgress",
		LatestToolLabel:       `sleep 20; printf 'slow-command-done\n'`,
		LatestToolStatus:      "running",
		LatestToolLiveCurrent: true,
		DetailItems: []model.DetailItem{
			{
				ID:     "tool-prev",
				Kind:   model.DetailItemTool,
				Label:  `printf 'previous\n'`,
				Status: "completed",
			},
		},
	}, time.Date(2026, time.May, 1, 23, 19, 21, 0, time.UTC))

	if !strings.Contains(text, "Last completed tool:") || !strings.Contains(text, "previous") {
		t.Fatalf("rendered tool = %q, want last completed foreign command", text)
	}
	if strings.Contains(text, "Current tool:") || strings.Contains(text, "slow-command-done") || strings.Contains(text, "Status: running") {
		t.Fatalf("rendered tool = %q, want foreign running command hidden", text)
	}
}

func TestRenderSummaryPanelShowsActiveRunElapsedTimeInHeader(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	now := time.Date(2026, time.May, 1, 23, 19, 21, 0, time.UTC)
	thread := model.Thread{
		ID:          "thread-active-no-tool",
		Title:       "Waiting command",
		ProjectName: "Codex",
	}
	snapshot := &appserver.ThreadReadSnapshot{
		Thread:              thread,
		LatestTurnID:        "turn-active-no-tool",
		LatestTurnStatus:    "inProgress",
		LatestTurnStartedAt: "2026-05-01T23:18:51Z",
	}
	messages := service.renderSummaryPanelMarkdownAt(context.Background(), thread, snapshot, nil, nil, now)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	text := messages[0].Text

	if !strings.Contains(text, "<b>处理中</b>") || !strings.Contains(text, "正在处理请求 · 30s") {
		t.Fatalf("rendered summary = %q, want stable Working status with elapsed time", text)
	}
	if strings.Contains(text, "No agent messages yet.") || strings.Contains(text, "Run active for:") {
		t.Fatalf("rendered summary = %q, want product copy instead of implementation state", text)
	}
}

func TestRenderOutputPanelEscapesHTMLInsideCodeBlock(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	thread := model.Thread{ID: "thread-output-html", Title: "Output HTML", ProjectName: "Codex"}
	text, _ := service.renderOutputPanel(context.Background(), thread, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-output-html",
		LatestToolLabel:  "printf html",
		LatestToolStatus: "completed",
		LatestToolOutput: "<tag>value & more</tag>\n",
	})
	if !hasHeaderKind(text, "Output") || !strings.Contains(text, "\n<pre><code>") {
		t.Fatalf("rendered output = %q, want plain header before HTML code block", text)
	}
	if !hasThreadChip(text, thread.ID) {
		t.Fatalf("rendered output = %q, want thread identity chip", text)
	}
	if strings.Contains(text, "<tag>value") {
		t.Fatalf("rendered output = %q, want escaped html", text)
	}
	if !strings.Contains(text, "&lt;tag&gt;value &amp; more&lt;/tag&gt;") {
		t.Fatalf("rendered output = %q, want escaped payload", text)
	}
}

func TestRenderOutputPanelFitsAfterHTMLEscaping(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	thread := model.Thread{ID: "thread-output-fit", Title: "Output fit", ProjectName: "Codex"}
	text, _ := service.renderOutputPanel(context.Background(), thread, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-output-fit",
		LatestToolLabel:  "printf fit",
		LatestToolStatus: "completed",
		LatestToolOutput: strings.Repeat("<tag>&", 2000),
	})
	if len(text) > outputMessageLimit {
		t.Fatalf("rendered output length = %d, want <= %d", len(text), outputMessageLimit)
	}
	if !hasHeaderKind(text, "Output") || !strings.Contains(text, "\n<pre><code>") {
		t.Fatalf("rendered output = %q, want plain header before HTML code block", text)
	}
	if !hasThreadChip(text, thread.ID) {
		t.Fatalf("rendered output = %q, want thread identity chip", text)
	}
}

func TestSyncThreadPanelToTargetSkipsRecentTerminalReplayFromBeforeObserveEnable(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-recent-before-enable",
		Title:       "Completed before observe",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Add(-30 * time.Second).Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetState(ctx, "observer.global_since_unix", strconv.FormatInt(time.Now().UTC().Unix(), 10)); err != nil {
		t.Fatalf("SetState(observer.global_since_unix) failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-before-enable",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-before-enable",
		LatestFinalText:  "Should stay quiet.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 0 {
		t.Fatalf("message count = %d, want 0 for completion from before /observe all; messages=%#v", len(sender.messages), sender.messages)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel != nil {
		t.Fatalf("expected no panel for completion from before /observe all, got %#v", panel)
	}
}

func TestLegacyTerminalPanelSeedsFinalFingerprintWithoutResending(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-legacy-terminal",
		Title:       "Legacy terminal",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-legacy",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-legacy",
		LatestFinalText:  "Legacy final text.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	panel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           target.ChatID,
		TopicID:          target.TopicID,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       "global_observer",
		SummaryMessageID: 11,
		ToolMessageID:    12,
		OutputMessageID:  13,
		CurrentTurnID:    "turn-legacy",
		Status:           "completed",
		ArchiveEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 0 {
		t.Fatalf("message count = %d, want 0 for legacy terminal replay; messages=%#v", len(sender.messages), sender.messages)
	}
	refreshed, err := service.store.GetThreadPanelByID(ctx, panel.ID)
	if err != nil {
		t.Fatalf("GetThreadPanelByID failed: %v", err)
	}
	if refreshed == nil || refreshed.LastFinalNoticeFP != "final-fp-legacy" {
		t.Fatalf("LastFinalNoticeFP = %#v, want final-fp-legacy", refreshed)
	}
}

func TestNewTerminalTurnAfterExistingPanelStillSendsFinal(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-new-terminal-final",
		Title:       "Fresh terminal after history",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            target.ChatID,
		TopicID:           target.TopicID,
		ProjectName:       thread.ProjectName,
		ThreadID:          thread.ID,
		SourceMode:        "global_observer",
		SummaryMessageID:  11,
		ToolMessageID:     12,
		OutputMessageID:   13,
		CurrentTurnID:     "turn-old",
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: "final-old",
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: model.Thread{
			ID:          thread.ID,
			Title:       thread.Title,
			ProjectName: thread.ProjectName,
			CWD:         thread.CWD,
			UpdatedAt:   thread.UpdatedAt,
		},
		LatestTurnID:     "turn-new",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-new",
		LatestFinalText:  "NEW FINAL",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	finalCount := len(finalMessages(sender.messages))
	if finalCount != 1 {
		t.Fatalf("final message count = %d, want 1; messages=%#v", finalCount, sender.messages)
	}
}

func TestFinalCardDetailsCallbacksEditSameMessageAndExportToolsFile(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-details",
		Title:       "Details flow",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-details",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-details",
		LatestFinalText:  "Done.",
		DetailItems: []model.DetailItem{
			{ID: "c1", Kind: model.DetailItemCommentary, Text: "First.", CommentaryIndex: 1},
			{ID: "c2", Kind: model.DetailItemCommentary, Text: "Second with `code`.", CommentaryIndex: 2},
			{ID: "t2", Kind: model.DetailItemTool, Label: "pwsh -Command rg test", Status: "completed", CommentaryIndex: 2},
			{ID: "o2", Kind: model.DetailItemOutput, Output: "match\n", CommentaryIndex: 2},
			{ID: "c3", Kind: model.DetailItemCommentary, Text: "Third.", CommentaryIndex: 3},
			{ID: "c4", Kind: model.DetailItemCommentary, Text: "Fourth.", CommentaryIndex: 4},
			{ID: "c5", Kind: model.DetailItemCommentary, Text: "Fifth.", CommentaryIndex: 5},
			{ID: "f1", Kind: model.DetailItemFinal, Text: "Done.", CommentaryIndex: 5},
		},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "explicit")
	finalCard := lastFinalMessage(t, sender.messages)
	if finalCard.options.Silent {
		t.Fatalf("final card = %#v, want audible Final", finalCard)
	}
	cardMessageID := finalCard.messageID
	detailsToken := buttonToken(finalCard.buttons, "📋 详情")
	if detailsToken == "" {
		t.Fatalf("final card buttons = %#v, want Details", finalCard.buttons)
	}

	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, detailsToken); err != nil {
		t.Fatalf("HandleCallback(details) failed: %v", err)
	}
	if len(sender.edits) < 1 {
		t.Fatalf("edits = %#v, want details edit", sender.edits)
	}
	details := sender.edits[len(sender.edits)-1]
	if details.messageID != cardMessageID {
		t.Fatalf("details message id = %d, want same %d", details.messageID, cardMessageID)
	}
	if !strings.Contains(details.text, "[commentary 1]") || !strings.Contains(details.text, "[commentary 4]") || strings.Contains(details.text, "Fifth.") {
		t.Fatalf("details page text = %q, want first four commentary entries only", details.text)
	}

	nextToken := buttonToken(details.buttons, ">")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, nextToken); err != nil {
		t.Fatalf("HandleCallback(next) failed: %v", err)
	}
	next := sender.edits[len(sender.edits)-1]
	if !strings.Contains(next.text, "[commentary 5]") || !strings.Contains(next.text, "Fifth.") {
		t.Fatalf("next details text = %q, want fifth commentary", next.text)
	}

	toolOnToken := buttonToken(details.buttons, "显示工具")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, toolOnToken); err != nil {
		t.Fatalf("HandleCallback(tool on) failed: %v", err)
	}
	toolMode := sender.edits[len(sender.edits)-1]
	if !strings.Contains(toolMode.text, "[Tool]") || !strings.Contains(toolMode.text, "[Output]") {
		t.Fatalf("tool mode text = %q, want related tool/output", toolMode.text)
	}

	toolNextToken := buttonToken(toolMode.buttons, ">")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, toolNextToken); err != nil {
		t.Fatalf("HandleCallback(tool next) failed: %v", err)
	}
	toolNext := sender.edits[len(sender.edits)-1]
	if !strings.Contains(toolNext.text, "[commentary 3]") || strings.Contains(toolNext.text, "[commentary 4]") {
		t.Fatalf("tool next text = %q, want exactly next commentary without skipping", toolNext.text)
	}

	fileToken := buttonToken(toolMode.buttons, "工具记录")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, fileToken); err != nil {
		t.Fatalf("HandleCallback(tools file) failed: %v", err)
	}
	if len(sender.documents) != 1 {
		t.Fatalf("documents = %#v, want one tools file", sender.documents)
	}
	if !sender.documents[0].options.Silent {
		t.Fatalf("document options = %#v, want silent explicit export", sender.documents[0].options)
	}
	if sender.documents[0].filePath != "" {
		t.Fatalf("tools file used path %q, want in-memory document data", sender.documents[0].filePath)
	}
	if !strings.Contains(string(sender.documents[0].data), "[Details tools]") || !strings.Contains(string(sender.documents[0].data), "[Tool]") {
		t.Fatalf("tools file data = %q, want details tool content", string(sender.documents[0].data))
	}

	backToken := buttonToken(toolMode.buttons, "返回")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, backToken); err != nil {
		t.Fatalf("HandleCallback(back) failed: %v", err)
	}
	back := sender.edits[len(sender.edits)-1]
	if !hasHeaderKind(back.text, "Final") {
		t.Fatalf("back text = %q, want final card", back.text)
	}
}

func TestFinalCardDetailsShowsToolOnlyTurnWithoutCommentary(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-tool-only-details",
		Title:       "Tool only details",
		ProjectName: "Codex",
		CWD:         "/Users/example/project",
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:             thread,
		LatestTurnID:       "turn-tool-only-details",
		LatestTurnStatus:   "completed",
		LatestFinalFP:      "final-tool-only",
		LatestFinalText:    "Done.",
		LatestToolID:       "cmd-sleep",
		LatestToolKind:     "commandExecution",
		LatestToolLabel:    "sleep 10",
		LatestToolStatus:   "completed",
		LatestToolFP:       "tool-sleep-completed",
		LatestProgressText: "completed: sleep 10",
		LatestProgressFP:   "progress-sleep-completed",
		DetailItems: []model.DetailItem{
			{ID: "user-1", Kind: model.DetailItemUser, Text: "Run sleep 10."},
			{ID: "cmd-sleep", Kind: model.DetailItemTool, Label: "sleep 10", Status: "completed", CommentaryIndex: 0, FP: "tool-sleep-completed"},
			{ID: "final-1", Kind: model.DetailItemFinal, Text: "Done.", CommentaryIndex: 0, FP: "final-tool-only"},
		},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "explicit")
	finalCard := lastFinalMessage(t, sender.messages)
	cardMessageID := finalCard.messageID
	detailsToken := buttonToken(finalCard.buttons, "📋 详情")
	if detailsToken == "" {
		t.Fatalf("final card buttons = %#v, want Details", finalCard.buttons)
	}

	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, detailsToken); err != nil {
		t.Fatalf("HandleCallback(details) failed: %v", err)
	}
	details := sender.edits[len(sender.edits)-1]
	if !strings.Contains(details.text, "Tool activity") || !strings.Contains(details.text, "sleep 10") || !strings.Contains(details.text, "Status: completed") {
		t.Fatalf("details text = %q, want tool-only command in default Details", details.text)
	}
	if strings.Contains(details.text, "No commentary entries") {
		t.Fatalf("details text = %q, want tool-only section instead of no commentary", details.text)
	}

	toolOnToken := buttonToken(details.buttons, "显示工具")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, toolOnToken); err != nil {
		t.Fatalf("HandleCallback(tool on) failed: %v", err)
	}
	toolMode := sender.edits[len(sender.edits)-1]
	if !strings.Contains(toolMode.text, "Tool activity") || !strings.Contains(toolMode.text, "[Tool]") || !strings.Contains(toolMode.text, "sleep 10") {
		t.Fatalf("tool mode text = %q, want orphan tool in tool mode", toolMode.text)
	}

	fileToken := buttonToken(toolMode.buttons, "工具记录")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, fileToken); err != nil {
		t.Fatalf("HandleCallback(tools file) failed: %v", err)
	}
	if len(sender.documents) != 1 {
		t.Fatalf("documents = %#v, want one tools file", sender.documents)
	}
	body := string(sender.documents[0].data)
	if !strings.Contains(body, "Tool activity") || !strings.Contains(body, "[Tool]") || !strings.Contains(body, "sleep 10") || !strings.Contains(body, "Status: completed") {
		t.Fatalf("tools file body = %q, want tool-only command", body)
	}
}

func TestDetailsCallbacksUsePanelTurnInsteadOfLatestThreadTurn(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	threadPayload := map[string]any{
		"id":     "thread-details-history",
		"name":   "Details history",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "completed",
		"turns": []any{
			map[string]any{
				"id":     "turn-old",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "old-c1", "type": "agentMessage", "phase": "commentary", "text": "Old commentary."},
					map[string]any{"id": "old-f1", "type": "agentMessage", "phase": "final_answer", "text": "Old final."},
				},
			},
			map[string]any{
				"id":     "turn-new",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "new-c1", "type": "agentMessage", "phase": "commentary", "text": "New commentary must not leak."},
					map[string]any{"id": "new-f1", "type": "agentMessage", "phase": "final_answer", "text": "New final must not leak."},
				},
			},
		},
	}
	latest := appserver.SnapshotFromThreadRead(threadPayload)
	latest.Thread.Raw = json.RawMessage(model.MustJSON(map[string]any{"thread": threadPayload}))
	if err := service.store.UpsertThread(ctx, latest.Thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.UpsertSnapshot(ctx, latest.Thread.ID, appserver.CompactSnapshot(nil, latest, time.Now().UTC())); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	panel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            123456789,
		TopicID:           0,
		ProjectName:       latest.Thread.ProjectName,
		ThreadID:          latest.Thread.ID,
		SourceMode:        model.PanelSourceExplicit,
		SummaryMessageID:  101,
		ToolMessageID:     102,
		OutputMessageID:   103,
		CurrentTurnID:     "turn-old",
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: "old-final-fp",
		LastFinalCardHash: "old-card-hash",
		DetailsViewJSON:   model.MustJSON(model.DetailsViewState{}),
		LastSummaryHash:   "old-card-hash",
		LastToolHash:      "old-tool",
		LastOutputHash:    "old-output",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	token := service.callbackButton(ctx, "Details", "details_open", latest.Thread.ID, "turn-old", "", map[string]any{"panel_id": panel.ID, "page": 0}).CallbackData

	if _, err := service.HandleCallback(ctx, 123456789, 0, 101, 123456789, token); err != nil {
		t.Fatalf("HandleCallback(details) failed: %v", err)
	}
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %#v, want one details edit", sender.edits)
	}
	text := sender.edits[0].text
	if !strings.Contains(text, "Old commentary.") || strings.Contains(text, "New commentary must not leak.") {
		t.Fatalf("details text = %q, want panel turn details only", text)
	}
}

func TestDetailsCallbacksStayBoundToOriginalPanelAfterNewerRunCompletes(t *testing.T) {
	t.Parallel()

	service, sender, thread, oldPanel, newPanel := setupDetailsHistoryPanels(t)
	ctx := context.Background()

	token := service.callbackButton(ctx, "Details", "details_open", thread.ID, "turn-old", "", map[string]any{"panel_id": oldPanel.ID, "page": 0}).CallbackData
	if _, err := service.HandleCallback(ctx, oldPanel.ChatID, oldPanel.TopicID, oldPanel.SummaryMessageID, oldPanel.ChatID, token); err != nil {
		t.Fatalf("HandleCallback(details) failed: %v", err)
	}
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %#v, want one old details edit", sender.edits)
	}
	details := sender.edits[0]
	if details.messageID != oldPanel.SummaryMessageID {
		t.Fatalf("details message id = %d, want old summary %d", details.messageID, oldPanel.SummaryMessageID)
	}
	if !strings.Contains(details.text, "Old commentary.") || strings.Contains(details.text, "New commentary must not leak.") {
		t.Fatalf("details text = %q, want old turn details only", details.text)
	}

	backToken := buttonToken(details.buttons, "返回")
	if backToken == "" {
		t.Fatalf("details buttons = %#v, want Back", details.buttons)
	}
	if _, err := service.HandleCallback(ctx, oldPanel.ChatID, oldPanel.TopicID, oldPanel.SummaryMessageID, oldPanel.ChatID, backToken); err != nil {
		t.Fatalf("HandleCallback(back) failed: %v", err)
	}
	if len(sender.edits) != 2 {
		t.Fatalf("edits = %#v, want details and back edits only", sender.edits)
	}
	back := sender.edits[1]
	if back.messageID != oldPanel.SummaryMessageID {
		t.Fatalf("back message id = %d, want old summary %d", back.messageID, oldPanel.SummaryMessageID)
	}
	if !hasHeaderKind(back.text, "Final") || !strings.Contains(back.text, "Old final.") || strings.Contains(back.text, "New final must not leak.") {
		t.Fatalf("back text = %q, want old final card only", back.text)
	}
	for _, edit := range sender.edits {
		if edit.messageID == newPanel.SummaryMessageID {
			t.Fatalf("new panel message was edited by old Details callback: %#v", edit)
		}
	}
	oldRoute, err := service.store.ResolveMessageRoute(ctx, oldPanel.ChatID, oldPanel.TopicID, oldPanel.SummaryMessageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute(old) failed: %v", err)
	}
	if oldRoute == nil || oldRoute.TurnID != "turn-old" {
		t.Fatalf("old message route = %#v, want turn-old", oldRoute)
	}
	newRoute, err := service.store.ResolveMessageRoute(ctx, newPanel.ChatID, newPanel.TopicID, newPanel.SummaryMessageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute(new) failed: %v", err)
	}
	if newRoute == nil || newRoute.TurnID != "turn-new" {
		t.Fatalf("new message route = %#v, want turn-new", newRoute)
	}
}

func TestDetailsCallbackWithoutPanelIDDoesNotFallbackToCurrentPanel(t *testing.T) {
	t.Parallel()

	service, sender, thread, oldPanel, _ := setupDetailsHistoryPanels(t)
	ctx := context.Background()

	token := service.callbackButton(ctx, "Details", "details_open", thread.ID, "turn-old", "", map[string]any{"page": 0}).CallbackData
	response, err := service.HandleCallback(ctx, oldPanel.ChatID, oldPanel.TopicID, oldPanel.SummaryMessageID, oldPanel.ChatID, token)
	if err != nil {
		t.Fatalf("HandleCallback(details missing panel) failed: %v", err)
	}
	if len(sender.edits) != 0 {
		t.Fatalf("edits = %#v, want no edit for missing panel_id", sender.edits)
	}
	if response == nil || !strings.Contains(response.Text, "详情卡片已失效") {
		t.Fatalf("response = %#v, want stale details response", response)
	}
}

func TestDetailsCallbackRejectsMismatchedMessageID(t *testing.T) {
	t.Parallel()

	service, sender, thread, oldPanel, newPanel := setupDetailsHistoryPanels(t)
	ctx := context.Background()

	token := service.callbackButton(ctx, "Details", "details_open", thread.ID, "turn-old", "", map[string]any{"panel_id": oldPanel.ID, "page": 0}).CallbackData
	response, err := service.HandleCallback(ctx, oldPanel.ChatID, oldPanel.TopicID, newPanel.SummaryMessageID, oldPanel.ChatID, token)
	if err != nil {
		t.Fatalf("HandleCallback(details wrong message) failed: %v", err)
	}
	if len(sender.edits) != 0 {
		t.Fatalf("edits = %#v, want no edit for mismatched details message", sender.edits)
	}
	if response == nil || !strings.Contains(response.Text, "详情卡片已失效") {
		t.Fatalf("response = %#v, want stale details response", response)
	}
}

func TestTurnOffPlanCallbackSetsDefaultOverrideAndEditsFinalCard(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	thread := model.Thread{
		ID:          "turn-off-plan-thread",
		Title:       "Turn off plan",
		ProjectName: "Codex",
		CWD:         "/Users/example/project",
		Status:      "idle",
	}
	snapshot := appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-plan-final",
		LatestTurnStatus: "completed",
		LatestFinalText:  "Plan mode final.",
		LatestFinalFP:    "turn-plan-final-fp",
		DetailItems: []model.DetailItem{
			{ID: "plan-1", Kind: model.DetailItemPlan, Text: "Plan text.", CommentaryIndex: 1},
			{ID: "final-1", Kind: model.DetailItemFinal, Text: "Plan mode final.", CommentaryIndex: 1},
		},
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.UpsertSnapshot(ctx, thread.ID, appserver.CompactSnapshot(nil, snapshot, time.Now().UTC())); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	panel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            123456789,
		TopicID:           0,
		ProjectName:       thread.ProjectName,
		ThreadID:          thread.ID,
		SourceMode:        model.PanelSourceExplicit,
		SummaryMessageID:  101,
		CurrentTurnID:     snapshot.LatestTurnID,
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: snapshot.LatestFinalFP,
		LastFinalCardHash: "old-card-hash",
		DetailsViewJSON:   model.MustJSON(model.DetailsViewState{}),
		LastSummaryHash:   "old-card-hash",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	_, buttons, _ := service.renderFinalCard(ctx, panel.ID, thread, &snapshot)
	token := buttonToken(buttons, "退出 Plan")
	if token == "" {
		t.Fatalf("final buttons = %#v, want Turn off Plan", buttons)
	}

	response, err := service.HandleCallback(ctx, panel.ChatID, panel.TopicID, panel.SummaryMessageID, panel.ChatID, token)
	if err != nil {
		t.Fatalf("HandleCallback(turn_off_plan) failed: %v", err)
	}
	if response == nil || !strings.Contains(response.CallbackText, "Plan Mode") {
		t.Fatalf("response = %#v, want callback text", response)
	}
	if got := service.threadCollaborationOverride(ctx, thread.ID); got != collaborationModeDefault {
		t.Fatalf("threadCollaborationOverride = %q, want default", got)
	}
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %#v, want one Final Card edit", sender.edits)
	}
	edit := sender.edits[0]
	if edit.messageID != panel.SummaryMessageID {
		t.Fatalf("edit message id = %d, want %d", edit.messageID, panel.SummaryMessageID)
	}
	if buttonToken(edit.buttons, "退出 Plan") != "" {
		t.Fatalf("edited buttons = %#v, want Turn off Plan removed", edit.buttons)
	}
}

func TestTurnOffPlanCallbackRejectsMismatchedMessageID(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	thread := model.Thread{
		ID:          "turn-off-plan-stale-thread",
		Title:       "Turn off plan stale",
		ProjectName: "Codex",
		CWD:         "/Users/example/project",
		Status:      "idle",
	}
	snapshot := appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-plan-final",
		LatestTurnStatus: "completed",
		LatestFinalText:  "Plan mode final.",
		LatestFinalFP:    "turn-plan-final-fp",
		DetailItems: []model.DetailItem{
			{ID: "plan-1", Kind: model.DetailItemPlan, Text: "Plan text.", CommentaryIndex: 1},
			{ID: "final-1", Kind: model.DetailItemFinal, Text: "Plan mode final.", CommentaryIndex: 1},
		},
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.UpsertSnapshot(ctx, thread.ID, appserver.CompactSnapshot(nil, snapshot, time.Now().UTC())); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	panel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            123456789,
		TopicID:           0,
		ProjectName:       thread.ProjectName,
		ThreadID:          thread.ID,
		SourceMode:        model.PanelSourceExplicit,
		SummaryMessageID:  101,
		CurrentTurnID:     snapshot.LatestTurnID,
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: snapshot.LatestFinalFP,
		LastFinalCardHash: "old-card-hash",
		DetailsViewJSON:   model.MustJSON(model.DetailsViewState{}),
		LastSummaryHash:   "old-card-hash",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	_, buttons, _ := service.renderFinalCard(ctx, panel.ID, thread, &snapshot)
	token := buttonToken(buttons, "退出 Plan")
	if token == "" {
		t.Fatalf("final buttons = %#v, want Turn off Plan", buttons)
	}

	response, err := service.HandleCallback(ctx, panel.ChatID, panel.TopicID, panel.SummaryMessageID+99, panel.ChatID, token)
	if err != nil {
		t.Fatalf("HandleCallback(turn_off_plan stale) failed: %v", err)
	}
	if len(sender.edits) != 0 {
		t.Fatalf("edits = %#v, want no edit for stale callback", sender.edits)
	}
	if got := service.threadCollaborationOverride(ctx, thread.ID); got != "" {
		t.Fatalf("threadCollaborationOverride = %q, want unchanged", got)
	}
	if response == nil || !strings.Contains(response.Text, "详情卡片已失效") {
		t.Fatalf("response = %#v, want stale response", response)
	}
}

func TestTurnOffPlanCallbackRejectsMismatchedPanelRoute(t *testing.T) {
	t.Parallel()

	service, sender, thread, panel, token := setupTurnOffPlanCallback(t, "turn-off-plan-route")
	ctx := context.Background()
	cases := []struct {
		name      string
		chatID    int64
		topicID   int64
		messageID int64
		token     string
	}{
		{
			name:      "wrong topic",
			chatID:    panel.ChatID,
			topicID:   panel.TopicID + 99,
			messageID: panel.SummaryMessageID,
			token:     token,
		},
		{
			name:      "wrong thread",
			chatID:    panel.ChatID,
			topicID:   panel.TopicID,
			messageID: panel.SummaryMessageID,
			token: service.callbackButton(ctx, "Turn off Plan", "turn_off_plan", thread.ID+"-other", panel.CurrentTurnID, "", map[string]any{
				"panel_id": panel.ID,
			}).CallbackData,
		},
		{
			name:      "wrong turn",
			chatID:    panel.ChatID,
			topicID:   panel.TopicID,
			messageID: panel.SummaryMessageID,
			token: service.callbackButton(ctx, "Turn off Plan", "turn_off_plan", thread.ID, panel.CurrentTurnID+"-other", "", map[string]any{
				"panel_id": panel.ID,
			}).CallbackData,
		},
		{
			name:      "wrong panel",
			chatID:    panel.ChatID,
			topicID:   panel.TopicID,
			messageID: panel.SummaryMessageID,
			token: service.callbackButton(ctx, "Turn off Plan", "turn_off_plan", thread.ID, panel.CurrentTurnID, "", map[string]any{
				"panel_id": panel.ID + 999,
			}).CallbackData,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := service.HandleCallback(ctx, tc.chatID, tc.topicID, tc.messageID, panel.ChatID, tc.token)
			if err != nil {
				t.Fatalf("HandleCallback(turn_off_plan stale) failed: %v", err)
			}
			if response == nil || !strings.Contains(response.Text, "详情卡片已失效") {
				t.Fatalf("response = %#v, want stale response", response)
			}
			if got := service.threadCollaborationOverride(ctx, thread.ID); got != "" {
				t.Fatalf("threadCollaborationOverride = %q, want unchanged", got)
			}
			if len(sender.edits) != 0 {
				t.Fatalf("edits = %#v, want no edit for stale callback", sender.edits)
			}
		})
	}
}

func TestDetailsToolsFileRejectsMismatchedPanelRoute(t *testing.T) {
	t.Parallel()

	service, sender, thread, oldPanel, _ := setupDetailsHistoryPanels(t)
	ctx := context.Background()

	token := service.callbackButton(ctx, "Tools file", "details_tools_file", thread.ID, "turn-new", "", map[string]any{"panel_id": oldPanel.ID, "commentary_index": 1}).CallbackData
	response, err := service.HandleCallback(ctx, oldPanel.ChatID, oldPanel.TopicID, oldPanel.SummaryMessageID, oldPanel.ChatID, token)
	if err != nil {
		t.Fatalf("HandleCallback(tools file wrong turn) failed: %v", err)
	}
	if len(sender.documents) != 0 {
		t.Fatalf("documents = %#v, want no export for mismatched details route", sender.documents)
	}
	if response == nil || !strings.Contains(response.Text, "详情卡片已失效") {
		t.Fatalf("response = %#v, want stale details response", response)
	}
}

func setupDetailsHistoryPanels(t *testing.T) (*Service, *recordingSender, model.Thread, *model.ThreadPanel, *model.ThreadPanel) {
	t.Helper()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	threadPayload := map[string]any{
		"id":     "thread-details-binding",
		"name":   "Details binding",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "completed",
		"turns": []any{
			map[string]any{
				"id":     "turn-old",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "old-c1", "type": "agentMessage", "phase": "commentary", "text": "Old commentary."},
					map[string]any{"id": "old-cmd", "type": "commandExecution", "command": "printf old", "status": "completed", "aggregatedOutput": "old output\n"},
					map[string]any{"id": "old-f1", "type": "agentMessage", "phase": "final_answer", "text": "Old final."},
				},
			},
			map[string]any{
				"id":     "turn-new",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "new-c1", "type": "agentMessage", "phase": "commentary", "text": "New commentary must not leak."},
					map[string]any{"id": "new-cmd", "type": "commandExecution", "command": "printf new", "status": "completed", "aggregatedOutput": "new output\n"},
					map[string]any{"id": "new-f1", "type": "agentMessage", "phase": "final_answer", "text": "New final must not leak."},
				},
			},
		},
	}
	latest := appserver.SnapshotFromThreadRead(threadPayload)
	latest.Thread.Raw = json.RawMessage(model.MustJSON(map[string]any{"thread": threadPayload}))
	if err := service.store.UpsertThread(ctx, latest.Thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.UpsertSnapshot(ctx, latest.Thread.ID, appserver.CompactSnapshot(nil, latest, time.Now().UTC())); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	oldPanel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            123456789,
		TopicID:           0,
		ProjectName:       latest.Thread.ProjectName,
		ThreadID:          latest.Thread.ID,
		SourceMode:        model.PanelSourceExplicit,
		SummaryMessageID:  101,
		ToolMessageID:     102,
		OutputMessageID:   103,
		CurrentTurnID:     "turn-old",
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: "old-final-fp",
		LastFinalCardHash: "old-card-hash",
		DetailsViewJSON:   model.MustJSON(model.DetailsViewState{}),
		LastSummaryHash:   "old-card-hash",
		LastToolHash:      "old-tool",
		LastOutputHash:    "old-output",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel(old) failed: %v", err)
	}
	newPanel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            123456789,
		TopicID:           0,
		ProjectName:       latest.Thread.ProjectName,
		ThreadID:          latest.Thread.ID,
		SourceMode:        model.PanelSourceExplicit,
		SummaryMessageID:  201,
		ToolMessageID:     202,
		OutputMessageID:   203,
		CurrentTurnID:     "turn-new",
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: "new-final-fp",
		LastFinalCardHash: "new-card-hash",
		DetailsViewJSON:   model.MustJSON(model.DetailsViewState{}),
		LastSummaryHash:   "new-card-hash",
		LastToolHash:      "new-tool",
		LastOutputHash:    "new-output",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel(new) failed: %v", err)
	}
	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{ChatID: oldPanel.ChatID, TopicID: oldPanel.TopicID, MessageID: oldPanel.SummaryMessageID, ThreadID: latest.Thread.ID, TurnID: "turn-old", EventID: "old-final-fp", CreatedAt: model.NowString()}); err != nil {
		t.Fatalf("PutMessageRoute(old) failed: %v", err)
	}
	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{ChatID: newPanel.ChatID, TopicID: newPanel.TopicID, MessageID: newPanel.SummaryMessageID, ThreadID: latest.Thread.ID, TurnID: "turn-new", EventID: "new-final-fp", CreatedAt: model.NowString()}); err != nil {
		t.Fatalf("PutMessageRoute(new) failed: %v", err)
	}
	return service, sender, latest.Thread, oldPanel, newPanel
}

func TestTerminalSyncAdoptsActivePanelWithoutTurnID(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-adopt-turn",
		Title:       "Adopt turn",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           target.ChatID,
		TopicID:          target.TopicID,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       "explicit",
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "",
		Status:           "inProgress",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-adopted",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-adopted",
		LatestFinalText:  "ADOPTED_OK",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 1 || len(sender.edits) != 1 {
		t.Fatalf("messages=%#v edits=%#v, want one in-place Final edit and one terminal notice", sender.messages, sender.edits)
	}
	if sender.messages[0].options.Silent || !strings.Contains(sender.messages[0].text, "<b>Adopt turn</b> 已完成") {
		t.Fatalf("terminal notice=%#v, want audible completion", sender.messages[0])
	}
	final := sender.edits[0]
	if final.messageID != 101 || !hasHeaderKind(final.text, "Final") {
		t.Fatalf("final = %#v, want Final on message 101", final)
	}
	if len(sender.deletes) != 0 {
		t.Fatalf("deletes = %#v, want no delete/re-send transition", sender.deletes)
	}
}

func TestSyncThreadPanelCreatesRouteablePlanPromptAndDedupes(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	service.cfg.NotifyNewRun = true
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-plan-card",
		Title:       "Plan card",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:              thread,
		LatestTurnID:        "turn-plan-card",
		LatestTurnStatus:    "active[waitingOnUserInput]",
		WaitingOnReply:      true,
		LatestProgressText:  "Need input.",
		LatestProgressFP:    "plan-progress-fp",
		LatestAgentMessages: []string{"Need input."},
		PlanPrompt: &model.PlanPrompt{
			PromptID:    "synthetic:thread-plan-card:turn-plan-card:abc",
			Source:      model.PromptSourceSyntheticPoll,
			ThreadID:    thread.ID,
			TurnID:      "turn-plan-card",
			ItemID:      "plan-item-1",
			Question:    "Choose next step?",
			Options:     []string{"Continue", "Revise"},
			Fingerprint: "plan-fp-1",
			Status:      "waiting for input",
		},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 {
		t.Fatalf("message count = %d, want one Needs input card across repeated sync: %#v", len(sender.messages), sender.messages)
	}
	if !hasHeaderKind(sender.messages[0].text, "Plan") {
		t.Fatalf("message = %q, want Needs input card", sender.messages[0].text)
	}
	if !strings.Contains(sender.messages[0].text, "Choose next step?") {
		t.Fatalf("plan prompt text = %q, want question", sender.messages[0].text)
	}
	if got := buttonToken(sender.messages[0].buttons, "Continue"); got == "" {
		t.Fatalf("plan prompt buttons = %#v, want structured Continue button", sender.messages[0].buttons)
	}
	if sender.messages[0].options.Silent {
		t.Fatalf("message options = %#v, want Needs input audible", sender.messages[0].options)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.UserMessageID != 0 || panel.PlanPromptMessageID != sender.messages[0].messageID || panel.LastPlanPromptFP != "plan-fp-1" {
		t.Fatalf("panel plan prompt state = %#v, want plan on summary id %d / plan-fp-1", panel, sender.messages[0].messageID)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, sender.messages[0].messageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute failed: %v", err)
	}
	if route == nil || route.ThreadID != thread.ID || route.TurnID != "turn-plan-card" || route.ItemID != "plan-item-1" || route.EventID != "plan-fp-1" {
		t.Fatalf("plan route = %#v, want thread/turn/item/fp route", route)
	}
}

func TestSyncThreadPanelCreatesServerRequestPlanPromptRoute(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-plan-request",
		Title:       "Plan request",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SavePendingApproval(ctx, model.PendingApproval{
		RequestID:   "request-plan-1",
		ThreadID:    thread.ID,
		TurnID:      "turn-plan-request",
		ItemID:      "request-item-1",
		PromptKind:  "user_input",
		Question:    "Pick deployment target?",
		PayloadJSON: `{"questions":[{"id":"target","header":"Target","question":"Pick deployment target?","options":[{"label":"staging","description":"Use staging."},{"label":"production","description":"Use production."}]}]}`,
		Status:      "pending",
		UpdatedAt:   model.NowString(),
	}); err != nil {
		t.Fatalf("SavePendingApproval failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-plan-request",
		LatestTurnStatus: "active[waitingOnUserInput]",
		WaitingOnReply:   true,
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 {
		t.Fatalf("message count = %d, want one Needs input card: %#v", len(sender.messages), sender.messages)
	}
	if got := buttonToken(sender.messages[0].buttons, "staging"); got == "" {
		t.Fatalf("plan prompt buttons = %#v, want staging button", sender.messages[0].buttons)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, sender.messages[0].messageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute failed: %v", err)
	}
	if route == nil || route.EventID != "plan_request:request-plan-1" {
		t.Fatalf("plan request route = %#v, want plan_request event id", route)
	}
}

func TestRunNoticeNotificationFlagCanSilenceNewRun(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	service.cfg.NotifyNewRun = false
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-silent-new-run",
		Title:       "Silent new run",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-silent-new-run",
		LatestTurnStatus: "inProgress",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].text, "<b>处理中</b>") {
		t.Fatalf("messages = %#v, want one Working card", sender.messages)
	}
	for index, message := range sender.messages {
		if !message.options.Silent {
			t.Fatalf("message[%d] options = %#v, want silent when notify_new_run is disabled", index, message.options)
		}
	}
}

func setupTurnOffPlanCallback(t *testing.T, suffix string) (*Service, *recordingSender, model.Thread, *model.ThreadPanel, string) {
	t.Helper()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	thread := model.Thread{
		ID:          suffix + "-thread",
		Title:       "Turn off plan",
		ProjectName: "Codex",
		CWD:         "/Users/example/project",
		Status:      "idle",
	}
	snapshot := appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     suffix + "-turn",
		LatestTurnStatus: "completed",
		LatestFinalText:  "Plan mode final.",
		LatestFinalFP:    suffix + "-fp",
		DetailItems: []model.DetailItem{
			{ID: suffix + "-plan", Kind: model.DetailItemPlan, Text: "Plan text.", CommentaryIndex: 1},
			{ID: suffix + "-final", Kind: model.DetailItemFinal, Text: "Plan mode final.", CommentaryIndex: 1},
		},
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.UpsertSnapshot(ctx, thread.ID, appserver.CompactSnapshot(nil, snapshot, time.Now().UTC())); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	panel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            123456789,
		TopicID:           0,
		ProjectName:       thread.ProjectName,
		ThreadID:          thread.ID,
		SourceMode:        model.PanelSourceExplicit,
		SummaryMessageID:  101,
		CurrentTurnID:     snapshot.LatestTurnID,
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: snapshot.LatestFinalFP,
		LastFinalCardHash: "old-card-hash",
		DetailsViewJSON:   model.MustJSON(model.DetailsViewState{}),
		LastSummaryHash:   "old-card-hash",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	_, buttons, _ := service.renderFinalCard(ctx, panel.ID, thread, &snapshot)
	token := buttonToken(buttons, "退出 Plan")
	if token == "" {
		t.Fatalf("final buttons = %#v, want Turn off Plan", buttons)
	}
	return service, sender, thread, panel, token
}

func buttonToken(rows [][]model.ButtonSpec, text string) string {
	for _, row := range rows {
		for _, button := range row {
			if button.Text == text {
				return button.CallbackData
			}
		}
	}
	return ""
}
