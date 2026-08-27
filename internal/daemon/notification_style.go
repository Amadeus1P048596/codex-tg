package daemon

import (
	"html"
	"strconv"
	"strings"

	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/tgformat"
)

type notificationState string

const (
	notificationRunning    notificationState = "running"
	notificationNeedsInput notificationState = "needs_input"
	notificationCompleted  notificationState = "completed"
	notificationFailed     notificationState = "failed"
	notificationCancelled  notificationState = "cancelled"
	notificationRequest    notificationState = "request"
)

type activityCardItem struct {
	Icon    string
	Text    string
	Command string
	Current bool
}

type notificationCardView struct {
	// Marker identifies the conversation. It must not encode task status.
	Marker     string
	Title      string
	State      notificationState
	Duration   string
	Summary    string
	Activities []activityCardItem
	Operations int
	Details    string
	ThreadID   string
	TurnID     string
}

func renderNotificationCard(view notificationCardView) model.RenderedMessage {
	marker := strings.TrimSpace(view.Marker)
	if marker == "" {
		marker = "❤️"
	}
	stateLabel := notificationStateLabel(view.State)
	duration := strings.TrimSpace(view.Duration)
	title := compactNotificationTitle(view.Title)

	statusHTML := "<b>Codex</b> · <b>" + stateLabel + "</b>"
	statusPlain := "Codex · " + stateLabel
	if duration != "" && notificationStateIsTerminal(view.State) {
		statusHTML += " · " + html.EscapeString(duration)
		statusPlain += " · " + duration
	}
	headerHTML := html.EscapeString(marker) + " " + statusHTML
	headerPlain := marker + " " + statusPlain
	if title != "" {
		headerHTML = html.EscapeString(marker) + " <b>" + html.EscapeString(title) + "</b>\n" + statusHTML
		headerPlain = marker + " " + title + "\n" + statusPlain
	}

	summary := cleanTelegramNilLiteral(view.Summary)
	if strings.TrimSpace(summary) == "" {
		summary = notificationEmptySummary(view.State)
	}
	visibleSummary, overflow := splitNotificationBody(summary)
	summaryHTML, summaryPlain := tgformat.MarkdownToHTML(visibleSummary)
	details := joinNotificationDetails(overflow, cleanTelegramNilLiteral(view.Details))
	detailsHTML, detailsPlain := tgformat.MarkdownToHTML(details)

	htmlParts := []string{headerHTML}
	plainParts := []string{headerPlain}
	if notificationStateShowsDurationLine(view.State) && duration != "" {
		htmlParts = append(htmlParts, summaryHTML+" · "+html.EscapeString(duration))
		plainParts = append(plainParts, summaryPlain+" · "+duration)
	} else {
		htmlParts = append(htmlParts, "", summaryHTML)
		plainParts = append(plainParts, "", summaryPlain)
	}

	activityHTML, activityPlain := renderNotificationActivities(view.Activities)
	if activityHTML != "" {
		htmlParts = append(htmlParts, "", activityHTML)
		plainParts = append(plainParts, "", activityPlain)
	}

	if detailsHTML != "" {
		htmlParts = append(htmlParts, "", "<blockquote expandable><b>详情</b>\n\n"+detailsHTML+"</blockquote>")
		plainParts = append(plainParts, "", "详情\n\n"+detailsPlain)
	}

	footerHTML, footerPlain := notificationFooter(view.Operations, view.ThreadID, view.TurnID)
	if footerHTML != "" {
		htmlParts = append(htmlParts, "", footerHTML)
		plainParts = append(plainParts, "", footerPlain)
	}
	return model.RenderedMessage{
		Text:      strings.Join(htmlParts, "\n"),
		ParseMode: "HTML",
		PlainText: strings.Join(plainParts, "\n"),
	}
}

func compactNotificationTitle(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(cleanTelegramNilLiteral(value))), " ")
	const limit = 96
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func renderNotificationActivities(items []activityCardItem) (string, string) {
	if len(items) == 0 {
		return "", ""
	}
	htmlLines := []string{"<b>进度</b>"}
	plainLines := []string{"进度"}
	for _, item := range items {
		text := strings.Join(strings.Fields(strings.TrimSpace(item.Text)), " ")
		if text == "" {
			continue
		}
		prefix := "✓ "
		if item.Current {
			prefix = "● " + strings.TrimSpace(item.Icon) + " "
		}
		prefix = strings.Join(strings.Fields(prefix), " ") + " "
		htmlLines = append(htmlLines, html.EscapeString(prefix+text))
		plainLines = append(plainLines, prefix+text)
		if item.Current {
			command := compactActivityCommand(item.Command, 180)
			if command != "" {
				htmlLines = append(htmlLines, "<code>"+html.EscapeString(command)+"</code>")
				plainLines = append(plainLines, command)
			}
		}
	}
	if len(htmlLines) == 1 {
		return "", ""
	}
	return strings.Join(htmlLines, "\n"), strings.Join(plainLines, "\n")
}

func notificationStateForStatus(status string, waitingForInput bool) notificationState {
	if waitingForInput {
		return notificationNeedsInput
	}
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch {
	case normalized == "completed" || normalized == "complete" || normalized == "success" || normalized == "succeeded":
		return notificationCompleted
	case strings.Contains(normalized, "fail") || strings.Contains(normalized, "error"):
		return notificationFailed
	case strings.Contains(normalized, "interrupt") || strings.Contains(normalized, "cancel") || normalized == "stopped":
		return notificationCancelled
	default:
		return notificationRunning
	}
}

func notificationStateLabel(state notificationState) string {
	switch state {
	case notificationNeedsInput:
		return "需要输入"
	case notificationCompleted:
		return "已完成"
	case notificationFailed:
		return "失败"
	case notificationCancelled:
		return "已取消"
	case notificationRequest:
		return "请求"
	default:
		return "处理中"
	}
}

func notificationStateIsTerminal(state notificationState) bool {
	return state == notificationCompleted || state == notificationFailed || state == notificationCancelled
}

func notificationStateShowsDurationLine(state notificationState) bool {
	return state == notificationRunning
}

func notificationEmptySummary(state notificationState) string {
	switch state {
	case notificationNeedsInput:
		return "等待你的输入"
	case notificationCompleted:
		return "任务已完成。"
	case notificationFailed:
		return "任务执行失败。"
	case notificationCancelled:
		return "任务已取消。"
	case notificationRequest:
		return "未读取到请求内容。"
	default:
		return "正在处理请求"
	}
}

func splitNotificationBody(value string) (string, string) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	if value == "" {
		return "", ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= 5 {
		return value, ""
	}
	return strings.TrimSpace(strings.Join(lines[:5], "\n")), strings.TrimSpace(strings.Join(lines[5:], "\n"))
}

func joinNotificationDetails(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n\n")
}

func notificationFooter(operations int, threadID, turnID string) (string, string) {
	parts := make([]string, 0, 3)
	if operations > 0 {
		parts = append(parts, strconv.Itoa(operations)+" 次操作")
	}
	if threadID = strings.TrimSpace(threadID); threadID != "" {
		parts = append(parts, "T:"+threadID)
	}
	if turnID = strings.TrimSpace(turnID); turnID != "" {
		parts = append(parts, "R:"+turnID)
	}
	if len(parts) == 0 {
		return "", ""
	}
	plain := strings.Join(parts, " · ")
	return "<code>" + html.EscapeString(plain) + "</code>", plain
}
