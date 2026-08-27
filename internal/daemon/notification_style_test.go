package daemon

import (
	"strings"
	"testing"
)

func TestRenderNotificationCardWorkingUsesConversationMarkerAndActivityHierarchy(t *testing.T) {
	t.Parallel()

	message := renderNotificationCard(notificationCardView{
		Marker:   "❤️",
		Title:    "codex-tg activity card",
		State:    notificationRunning,
		Duration: "1m 06s",
		Summary:  "正在处理请求",
		Activities: []activityCardItem{
			{Text: "修改输出裁剪逻辑"},
			{Text: "格式化代码"},
			{Icon: "🧪", Text: "运行测试", Command: `go test ./internal/daemon/...`, Current: true},
		},
		Operations: 16,
		ThreadID:   "01a03706",
	})

	want := strings.Join([]string{
		"❤️ <b>codex-tg activity card</b>",
		"<b>Codex</b> · <b>处理中</b>",
		"正在处理请求 · 1m 06s",
		"",
		"<b>进度</b>",
		"✓ 修改输出裁剪逻辑",
		"✓ 格式化代码",
		"● 🧪 运行测试",
		"<code>go test ./internal/daemon/...</code>",
		"",
		"<code>16 次操作 · T:01a03706</code>",
	}, "\n")
	if message.Text != want {
		t.Fatalf("notification HTML mismatch\n--- got ---\n%s\n--- want ---\n%s", message.Text, want)
	}
	if message.ParseMode != "HTML" {
		t.Fatalf("ParseMode = %q, want HTML", message.ParseMode)
	}
}

func TestRenderNotificationCardCompletedKeepsMarkerAndLowWeightMetadata(t *testing.T) {
	t.Parallel()

	message := renderNotificationCard(notificationCardView{
		Marker:     "❤️",
		Title:      "codex-tg isolation",
		State:      notificationCompleted,
		Duration:   "1m 21s",
		Summary:    "已重构 daemon 输出裁剪逻辑，并通过相关测试。",
		Details:    "**Changes**\n• 合并重复逻辑\n• 调整 Telegram 输出截断",
		Operations: 18,
		ThreadID:   "01a03706",
		TurnID:     "01a037d2",
	})

	for _, want := range []string{
		"❤️ <b>codex-tg isolation</b>\n<b>Codex</b> · <b>已完成</b> · 1m 21s",
		"已重构 daemon 输出裁剪逻辑，并通过相关测试。",
		"<blockquote expandable><b>详情</b>",
		"<b>Changes</b>",
		"<code>18 次操作 · T:01a03706 · R:01a037d2</code>",
	} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("notification HTML missing %q:\n%s", want, message.Text)
		}
	}
	if strings.Contains(message.Text, "✅") || strings.Contains(message.Text, "🔵") {
		t.Fatalf("notification must not replace the conversation marker with status emoji: %s", message.Text)
	}
}

func TestRenderNotificationCardEscapesDynamicHTML(t *testing.T) {
	t.Parallel()

	message := renderNotificationCard(notificationCardView{
		Marker:   `<&>`,
		Title:    `task <title> & review`,
		State:    notificationFailed,
		Summary:  `Could not open C:\work\a<b>.txt & retry.`,
		Details:  `<script>alert("x")</script>`,
		ThreadID: `t<&`,
		TurnID:   `r>&`,
	})

	for _, want := range []string{
		`&lt;&amp;&gt; <b>task &lt;title&gt; &amp; review</b>`,
		`<b>Codex</b> · <b>失败</b>`,
		`Could not open C:\work\a&lt;b&gt;.txt &amp; retry.`,
		`&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`,
		`<code>T:t&lt;&amp; · R:r&gt;&amp;</code>`,
	} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("notification HTML missing %q:\n%s", want, message.Text)
		}
	}
	if strings.Contains(message.Text, "<script>") {
		t.Fatalf("notification contains unescaped script tag: %s", message.Text)
	}
}

func TestRenderNotificationCardWithoutTitleUsesCompactStatusHeader(t *testing.T) {
	t.Parallel()

	message := renderNotificationCard(notificationCardView{
		Marker:  "❤️",
		State:   notificationRunning,
		Summary: "正在处理请求",
	})
	if !strings.HasPrefix(message.Text, "❤️ <b>Codex</b> · <b>处理中</b>\n") {
		t.Fatalf("message = %q, want compact status fallback", message.Text)
	}
	if strings.Contains(message.Text, "<b></b>") {
		t.Fatalf("message = %q, must not render an empty title", message.Text)
	}
}

func TestSplitNotificationBodyLimitsVisibleSummaryToFiveLines(t *testing.T) {
	t.Parallel()

	summary, details := splitNotificationBody("one\ntwo\nthree\nfour\nfive\nsix\nseven")
	if summary != "one\ntwo\nthree\nfour\nfive" {
		t.Fatalf("summary = %q", summary)
	}
	if details != "six\nseven" {
		t.Fatalf("details = %q", details)
	}
}

func TestNotificationStateNormalizesRuntimeStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		wait   bool
		want   notificationState
	}{
		{status: "inProgress", want: notificationRunning},
		{status: "completed", want: notificationCompleted},
		{status: "failed", want: notificationFailed},
		{status: "interrupted", want: notificationCancelled},
		{status: "inProgress", wait: true, want: notificationNeedsInput},
	}
	for _, test := range tests {
		if got := notificationStateForStatus(test.status, test.wait); got != test.want {
			t.Fatalf("status=%q wait=%v => %q, want %q", test.status, test.wait, got, test.want)
		}
	}
}
