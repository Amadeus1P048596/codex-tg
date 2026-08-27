package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/tgformat"
)

const (
	visualMarkerStatePrefix = "ui.thread_marker."
	visualMarkerTTL         = 30 * time.Minute
	visualProjectMaxRunes   = 18
	visualThreadMaxRunes    = 30
)

var visualMarkerPalette = []string{
	"🟦", "🟩", "🟨", "🟧", "🟥", "🟪", "⬛", "⬜", "🟫",
	"🔵", "🟢", "🟡", "🟠", "🔴", "🟣", "⚫", "⚪", "🟤",
	"💙", "💚", "💛", "🧡", "❤️", "💜", "🖤", "🤍", "🤎", "🩷", "🩵", "🩶",
}

type visualMarkerAssignment struct {
	Marker        string `json:"marker"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

type visualCardHeaderView struct {
	Text     string
	Entities []model.MessageEntity
}

type visualCardHeaderBuilder struct {
	text     strings.Builder
	entities []model.MessageEntity
}

func (s *Service) visualCardHeader(ctx context.Context, kind string, thread model.Thread, turnID, status, timing string) visualCardHeaderView {
	marker := s.visualMarker(ctx, thread.ID)
	project := compactVisualLabel(firstNonEmpty(thread.ProjectName, "Project"), visualProjectMaxRunes)
	title := compactVisualLabel(firstNonEmpty(thread.Title, thread.ShortID()), visualThreadMaxRunes)
	role := visualRole(kind)

	var builder visualCardHeaderBuilder
	builder.add(marker+" ", "")
	builder.add(title, "bold")
	builder.add("\n", "")
	builder.add(role, "bold")
	if status = strings.TrimSpace(status); status != "" {
		builder.add(" · ", "")
		builder.add(visualStatus(status), "bold")
	}
	if timing = strings.TrimSpace(timing); timing != "" {
		builder.add(" · ", "")
		builder.add("⏱ "+timing, "bold")
	}
	builder.add("\n", "")
	metadata := []string{project, "T:" + visualShortID(thread.ID)}
	if shortTurnID := visualShortID(turnID); shortTurnID != "" {
		metadata = append(metadata, "R:"+shortTurnID)
	}
	builder.add(strings.Join(metadata, " · "), "code")
	return visualCardHeaderView{Text: builder.text.String(), Entities: builder.entities}
}

func visualRole(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "user":
		return "👤 [User]"
	case "final":
		return "🤖 [Final]"
	case "commentary":
		return "💬 [commentary]"
	case "plan":
		return "🧭 [Plan]"
	case "details":
		return "📋 [Details]"
	default:
		kind = strings.TrimSpace(kind)
		if kind == "" {
			kind = "Message"
		}
		return "💬 " + kind
	}
}

func visualStatus(status string) string {
	status = strings.TrimSpace(status)
	lower := strings.ToLower(status)
	switch {
	case lower == "completed" || lower == "complete" || lower == "success":
		return "✅ Status: " + status
	case strings.Contains(lower, "progress") || lower == "active" || lower == "running":
		return "⏳ Status: " + status
	case strings.Contains(lower, "waiting") || strings.Contains(lower, "input"):
		return "❓ Status: " + status
	case strings.Contains(lower, "fail") || strings.Contains(lower, "error") || lower == "interrupted":
		return "⚠️ Status: " + status
	default:
		return "Status: " + status
	}
}

func (b *visualCardHeaderBuilder) add(text, entityType string) {
	if text == "" {
		return
	}
	offset := visualUTF16Len(b.text.String())
	b.text.WriteString(text)
	if entityType == "" {
		return
	}
	b.entities = append(b.entities, model.MessageEntity{
		Type:   entityType,
		Offset: offset,
		Length: visualUTF16Len(text),
	})
}

func visualUTF16Len(text string) int {
	length := 0
	for _, value := range text {
		length += utf16.RuneLen(value)
	}
	return length
}

func renderMarkdownWithVisualHeader(header visualCardHeaderView, markdown string) []model.RenderedMessage {
	return applyVisualHeaderEntities(tgformat.RenderMarkdownWithHeader(header.Text, markdown), header)
}

func renderSegmentsWithVisualHeader(header visualCardHeaderView, segments []tgformat.Segment, maxLen int) []model.RenderedMessage {
	return applyVisualHeaderEntities(tgformat.RenderSegments(segments, maxLen), header)
}

func applyVisualHeaderEntities(messages []model.RenderedMessage, header visualCardHeaderView) []model.RenderedMessage {
	if len(header.Entities) == 0 {
		return messages
	}
	for index := range messages {
		if !strings.HasPrefix(messages[index].Text, header.Text) {
			continue
		}
		entities := make([]model.MessageEntity, 0, len(header.Entities)+len(messages[index].Entities))
		entities = append(entities, header.Entities...)
		entities = append(entities, messages[index].Entities...)
		messages[index].Entities = entities
	}
	return messages
}

func (s *Service) visualHeader(ctx context.Context, kind string, thread model.Thread, turnID string) string {
	marker := s.visualMarker(ctx, thread.ID)
	project := compactVisualLabel(firstNonEmpty(thread.ProjectName, "Project"), visualProjectMaxRunes)
	title := compactVisualLabel(firstNonEmpty(thread.Title, thread.ShortID()), visualThreadMaxRunes)
	parts := []string{
		marker,
		fmt.Sprintf("[%s]", project),
		fmt.Sprintf("[%s]", title),
		fmt.Sprintf("[T:%s]", visualShortID(thread.ID)),
	}
	if shortTurnID := visualShortID(turnID); shortTurnID != "" {
		parts = append(parts, fmt.Sprintf("[R:%s]", shortTurnID))
	}
	if kind = strings.TrimSpace(kind); kind != "" {
		parts = append(parts, fmt.Sprintf("[%s]", kind))
	}
	return strings.Join(parts, " ")
}

func visualFileHeader(thread model.Thread, turnID, kind string) string {
	marker := visualMarkerPalette[visualHashIndex(thread.ID, len(visualMarkerPalette))]
	project := compactVisualLabel(firstNonEmpty(thread.ProjectName, "Project"), visualProjectMaxRunes)
	title := compactVisualLabel(firstNonEmpty(thread.Title, thread.ShortID()), visualThreadMaxRunes)
	parts := []string{
		marker,
		fmt.Sprintf("[%s]", project),
		fmt.Sprintf("[%s]", title),
		fmt.Sprintf("[T:%s]", visualShortID(thread.ID)),
	}
	if shortTurnID := visualShortID(turnID); shortTurnID != "" {
		parts = append(parts, fmt.Sprintf("[R:%s]", shortTurnID))
	}
	if kind = strings.TrimSpace(kind); kind != "" {
		parts = append(parts, fmt.Sprintf("[%s]", kind))
	}
	return strings.Join(parts, " ")
}

func (s *Service) visualDividerText(ctx context.Context, thread model.Thread, turnID string) string {
	marker := s.visualMarker(ctx, thread.ID)
	project := compactVisualLabel(firstNonEmpty(thread.ProjectName, "Project"), visualProjectMaxRunes)
	title := compactVisualLabel(firstNonEmpty(thread.Title, thread.ShortID()), visualThreadMaxRunes)
	parts := []string{
		marker,
		"New run:",
		fmt.Sprintf("[%s]", project),
		fmt.Sprintf("[%s]", title),
		fmt.Sprintf("[T:%s]", visualShortID(thread.ID)),
	}
	if shortTurnID := visualShortID(turnID); shortTurnID != "" {
		parts = append(parts, fmt.Sprintf("[R:%s]", shortTurnID))
	}
	return strings.Join(parts, " ")
}

func (s *Service) visualMarker(ctx context.Context, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return visualMarkerPalette[0]
	}
	fallback := visualMarkerPalette[visualHashIndex(threadID, len(visualMarkerPalette))]
	now := time.Now().UTC()
	states, err := s.store.ListState(ctx)
	if err != nil {
		return fallback
	}
	assignments := map[string]visualMarkerAssignment{}
	occupied := map[string]string{}
	for key, raw := range states {
		if !strings.HasPrefix(key, visualMarkerStatePrefix) {
			continue
		}
		owner := strings.TrimPrefix(key, visualMarkerStatePrefix)
		var assignment visualMarkerAssignment
		if err := json.Unmarshal([]byte(raw), &assignment); err != nil {
			continue
		}
		if strings.TrimSpace(assignment.Marker) == "" || assignment.ExpiresAtUnix <= now.Unix() {
			continue
		}
		assignments[owner] = assignment
		if owner != threadID {
			occupied[assignment.Marker] = owner
		}
	}
	marker := ""
	if existing, ok := assignments[threadID]; ok && occupied[existing.Marker] == "" {
		marker = existing.Marker
	}
	if marker == "" {
		marker = chooseVisualMarker(threadID, occupied)
	}
	payload, err := json.Marshal(visualMarkerAssignment{
		Marker:        marker,
		ExpiresAtUnix: now.Add(visualMarkerTTL).Unix(),
	})
	if err == nil {
		_ = s.store.SetState(ctx, visualMarkerStatePrefix+threadID, string(payload))
	}
	return marker
}

func chooseVisualMarker(threadID string, occupied map[string]string) string {
	start := visualHashIndex(threadID, len(visualMarkerPalette))
	for offset := 0; offset < len(visualMarkerPalette); offset++ {
		marker := visualMarkerPalette[(start+offset)%len(visualMarkerPalette)]
		if occupied[marker] == "" {
			return marker
		}
	}
	base := visualMarkerPalette[start]
	for suffix := 2; ; suffix++ {
		marker := fmt.Sprintf("%s#%d", base, suffix)
		if occupied[marker] == "" {
			return marker
		}
	}
}

func visualHashIndex(value string, modulo int) int {
	if modulo <= 0 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(value))
	return int(hasher.Sum32() % uint32(modulo))
}

func compactVisualLabel(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "-"
	}
	if maxRunes <= 3 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maxRunes-3])) + "..."
}

func visualShortID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if segment, _, ok := strings.Cut(value, "-"); ok && segment != "" {
		value = segment
	}
	if strings.HasPrefix(value, "019d") && len(value) >= 8 {
		return value[len(value)-4:]
	}
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}
