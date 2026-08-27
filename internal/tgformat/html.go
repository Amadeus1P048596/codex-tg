package tgformat

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"unicode/utf16"

	tgmd "github.com/eekstunt/telegramify-markdown-go"

	"github.com/mideco-tech/codex-tg/internal/model"
)

type htmlEntityRange struct {
	start int
	end   int
	open  string
	close string
}

// MarkdownToHTML converts CommonMark input to renderer-owned Telegram HTML and
// returns the matching markup-free fallback text.
func MarkdownToHTML(markdown string) (string, string) {
	converted := tgmd.Convert(protectRawHTMLFromMarkdown(strings.TrimSpace(markdown)))
	converted.Text = restoreProtectedHTMLCharacters(converted.Text)
	rendered := model.RenderedMessage{Text: converted.Text, Entities: convertEntities(converted.Entities)}
	return RenderedToHTML(rendered), converted.Text
}

const (
	protectedLessThan    = "\ue000"
	protectedGreaterThan = "\ue001"
)

func protectRawHTMLFromMarkdown(value string) string {
	return strings.NewReplacer("<", protectedLessThan, ">", protectedGreaterThan).Replace(value)
}

func restoreProtectedHTMLCharacters(value string) string {
	return strings.NewReplacer(protectedLessThan, "<", protectedGreaterThan, ">").Replace(value)
}

// RenderedToHTML converts a plain Telegram entity message into safe Telegram
// HTML. Text and attribute values are escaped before trusted tags are inserted.
func RenderedToHTML(message model.RenderedMessage) string {
	ranges := make([]htmlEntityRange, 0, len(message.Entities))
	for _, entity := range message.Entities {
		open, close, ok := telegramHTMLTags(entity)
		if !ok || entity.Offset < 0 || entity.Length <= 0 {
			continue
		}
		ranges = append(ranges, htmlEntityRange{
			start: entity.Offset,
			end:   entity.Offset + entity.Length,
			open:  open,
			close: close,
		})
	}
	openings := map[int][]htmlEntityRange{}
	closings := map[int][]htmlEntityRange{}
	for _, entity := range ranges {
		openings[entity.start] = append(openings[entity.start], entity)
		closings[entity.end] = append(closings[entity.end], entity)
	}
	for offset := range openings {
		sort.SliceStable(openings[offset], func(i, j int) bool {
			return openings[offset][i].end > openings[offset][j].end
		})
	}
	for offset := range closings {
		sort.SliceStable(closings[offset], func(i, j int) bool {
			return closings[offset][i].start > closings[offset][j].start
		})
	}

	var output strings.Builder
	utf16Offset := 0
	writeClosings := func(offset int) {
		for _, entity := range closings[offset] {
			output.WriteString(entity.close)
		}
	}
	writeOpenings := func(offset int) {
		for _, entity := range openings[offset] {
			output.WriteString(entity.open)
		}
	}
	for _, value := range message.Text {
		writeClosings(utf16Offset)
		writeOpenings(utf16Offset)
		output.WriteString(html.EscapeString(string(value)))
		utf16Offset += utf16.RuneLen(value)
	}
	writeClosings(utf16Offset)
	writeOpenings(utf16Offset)
	return output.String()
}

func telegramHTMLTags(entity model.MessageEntity) (string, string, bool) {
	switch entity.Type {
	case "bold":
		return "<b>", "</b>", true
	case "italic":
		return "<i>", "</i>", true
	case "underline":
		return "<u>", "</u>", true
	case "strikethrough":
		return "<s>", "</s>", true
	case "spoiler":
		return "<tg-spoiler>", "</tg-spoiler>", true
	case "code":
		return "<code>", "</code>", true
	case "pre":
		if language := safeLanguage(entity.Language); language != "" {
			return `<pre><code class="language-` + language + `">`, "</code></pre>", true
		}
		return "<pre>", "</pre>", true
	case "text_link":
		return `<a href="` + html.EscapeString(entity.URL) + `">`, "</a>", true
	case "blockquote":
		return "<blockquote>", "</blockquote>", true
	case "expandable_blockquote":
		return "<blockquote expandable>", "</blockquote>", true
	default:
		return "", "", false
	}
}

func safeLanguage(value string) string {
	value = strings.TrimSpace(value)
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return ""
	}
	if len(value) > 40 {
		return ""
	}
	return value
}

func RenderHTML(markdown string) model.RenderedMessage {
	htmlText, plainText := MarkdownToHTML(markdown)
	return model.RenderedMessage{
		Text:      htmlText,
		ParseMode: "HTML",
		PlainText: plainText,
	}
}

func ValidateHTMLLength(message model.RenderedMessage, maxLen int) error {
	if maxLen > 0 && tgmd.UTF16Len(message.PlainText) > maxLen {
		return fmt.Errorf("telegram text is %d UTF-16 units, limit is %d", tgmd.UTF16Len(message.PlainText), maxLen)
	}
	return nil
}
