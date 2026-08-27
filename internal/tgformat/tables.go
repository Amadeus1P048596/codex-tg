package tgformat

import (
	"regexp"
	"strconv"
	"strings"
)

var markdownTableDelimiterCell = regexp.MustCompile(`^:?-{3,}:?$`)

// telegramReadableMarkdownTables rewrites GitHub-style pipe tables as labeled
// records. Telegram has no table entity, and leaving table alignment to a
// proportional font makes even small tables difficult to read on a phone.
func telegramReadableMarkdownTables(markdown string) string {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	fenceMarker := ""
	for index := 0; index < len(lines); {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if marker, ok := markdownFenceMarker(trimmed); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			out = append(out, line)
			index++
			continue
		}
		if inFence || index+1 >= len(lines) {
			out = append(out, line)
			index++
			continue
		}

		headers, headerOK := splitMarkdownTableRow(line)
		delimiters, delimiterOK := splitMarkdownTableRow(lines[index+1])
		if !headerOK || !delimiterOK || len(headers) != len(delimiters) || !isMarkdownTableDelimiter(delimiters) {
			out = append(out, line)
			index++
			continue
		}

		bodyEnd := index + 2
		rows := make([][]string, 0)
		for bodyEnd < len(lines) {
			row, ok := splitMarkdownTableRow(lines[bodyEnd])
			if !ok {
				break
			}
			rows = append(rows, row)
			bodyEnd++
		}
		if len(rows) == 0 {
			out = append(out, line)
			index++
			continue
		}

		for rowIndex, row := range rows {
			if rowIndex > 0 {
				out = append(out, "")
			}
			title := "记录 " + strconv.Itoa(rowIndex+1)
			if len(row) > 0 && strings.TrimSpace(row[0]) != "" {
				title = strings.TrimSpace(row[0])
			}
			out = append(out, "- **"+title+"**")

			columns := len(headers)
			if len(row) > columns {
				columns = len(row)
			}
			for column := 1; column < columns; column++ {
				label := ""
				if column < len(headers) {
					label = strings.TrimSpace(headers[column])
				}
				if label == "" {
					label = "列 " + strconv.Itoa(column+1)
				}
				value := "—"
				if column < len(row) && strings.TrimSpace(row[column]) != "" {
					value = strings.TrimSpace(row[column])
				}
				out = append(out, "  **"+label+"：** "+value)
			}
		}
		index = bodyEnd
	}
	return strings.Join(out, "\n")
}

func markdownFenceMarker(line string) (string, bool) {
	if strings.HasPrefix(line, "```") {
		return "`", true
	}
	if strings.HasPrefix(line, "~~~") {
		return "~", true
	}
	return "", false
}

func splitMarkdownTableRow(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil, false
	}
	cells := make([]string, 0, 4)
	var cell strings.Builder
	escaped := false
	inCode := false
	separators := 0
	for _, value := range trimmed {
		switch {
		case escaped:
			cell.WriteRune(value)
			escaped = false
		case value == '\\':
			cell.WriteRune(value)
			escaped = true
		case value == '`':
			inCode = !inCode
			cell.WriteRune(value)
		case value == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			separators++
		default:
			cell.WriteRune(value)
		}
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	if separators == 0 {
		return nil, false
	}
	if len(cells) > 0 && cells[0] == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	if len(cells) < 2 {
		return nil, false
	}
	return cells, true
}

func isMarkdownTableDelimiter(cells []string) bool {
	for _, cell := range cells {
		if !markdownTableDelimiterCell.MatchString(strings.TrimSpace(cell)) {
			return false
		}
	}
	return len(cells) > 0
}
