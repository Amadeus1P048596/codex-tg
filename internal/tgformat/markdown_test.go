package tgformat

import (
	"strings"
	"testing"

	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestRenderMarkdownWithHeaderKeepsHeaderPlainAndConvertsCodeFence(t *testing.T) {
	t.Parallel()

	messages := RenderMarkdownWithHeader("[Final] [Project: Codex] [Thread: Найти *Swagger* [Stellar]]\nStatus: completed", "Run `rg`:\n\n```bash\nrg -n 'Authorization' stellar_ws.txt\n```\n\n- done")
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message := messages[0]
	if !strings.Contains(message.Text, "[Thread: Найти *Swagger* [Stellar]]") {
		t.Fatalf("header was not preserved as plain text: %q", message.Text)
	}
	if strings.Contains(message.Text, "```") {
		t.Fatalf("rendered text still contains raw markdown fence: %q", message.Text)
	}
	if !hasEntity(message.Entities, "code", "") {
		t.Fatalf("entities = %#v, want inline code entity", message.Entities)
	}
	if !hasEntity(message.Entities, "pre", "bash") {
		t.Fatalf("entities = %#v, want bash pre entity", message.Entities)
	}
}

func TestRenderSegmentsSplitsLongMarkdown(t *testing.T) {
	t.Parallel()

	messages := RenderSegments([]Segment{
		Plain("[Final]\n\n"),
		Markdown(strings.Repeat("line with `code`\n", 500)),
	}, 512)
	if len(messages) < 2 {
		t.Fatalf("len(messages) = %d, want split messages", len(messages))
	}
	for _, message := range messages {
		if len(message.Text) == 0 {
			t.Fatal("split message text must not be empty")
		}
	}
}

func TestRenderSegmentsConvertsPipeTableToTelegramReadableRecords(t *testing.T) {
	t.Parallel()

	messages := RenderSegments([]Segment{Markdown(strings.Join([]string{
		"| 检查项 | 结果 | 说明 |",
		"| --- | :---: | --- |",
		"| App Server | ✅ | `stdio` |",
		"| 私钥 | 未发现 | 0 命中 |",
	}, "\n"))}, TelegramMessageLimit)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message := messages[0]
	if strings.Contains(message.Text, "| ---") || strings.Contains(message.Text, "| 检查项 |") {
		t.Fatalf("rendered text still contains a raw Markdown table: %q", message.Text)
	}
	for _, want := range []string{"检查项：App Server", "结果：✅", "说明：stdio", "检查项：私钥", "结果：未发现", "说明：0 命中"} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("rendered text = %q, want labeled field %q", message.Text, want)
		}
	}
	if !hasEntity(message.Entities, "code", "") {
		t.Fatalf("entities = %#v, want table cell inline code entity", message.Entities)
	}
}

func TestRenderSegmentsLeavesPipeTableSyntaxInsideCodeFenceUntouched(t *testing.T) {
	t.Parallel()

	messages := RenderSegments([]Segment{Markdown("```text\n| key | value |\n| --- | --- |\n| a | b |\n```")}, TelegramMessageLimit)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if !strings.Contains(messages[0].Text, "| --- | --- |") {
		t.Fatalf("code-fenced table syntax was changed: %q", messages[0].Text)
	}
	if !hasEntity(messages[0].Entities, "pre", "text") {
		t.Fatalf("entities = %#v, want text pre entity", messages[0].Entities)
	}
}

func TestMarkdownToHTMLConvertsPipeTableToTelegramReadableRecords(t *testing.T) {
	t.Parallel()

	htmlText, plainText := MarkdownToHTML("| 项目 | 状态 |\n| --- | --- |\n| 图片 | `保留` |")
	if strings.Contains(htmlText, "| ---") || strings.Contains(plainText, "| ---") {
		t.Fatalf("MarkdownToHTML left raw table syntax: html=%q plain=%q", htmlText, plainText)
	}
	for _, want := range []string{"项目：图片", "状态：保留"} {
		if !strings.Contains(plainText, want) {
			t.Fatalf("plainText = %q, want %q", plainText, want)
		}
	}
	if !strings.Contains(htmlText, "<code>保留</code>") {
		t.Fatalf("htmlText = %q, want inline code", htmlText)
	}
}

func hasEntity(entities []model.MessageEntity, entityType, language string) bool {
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
