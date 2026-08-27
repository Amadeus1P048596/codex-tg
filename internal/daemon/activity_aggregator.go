package daemon

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const activityHistoryLimit = 3

var activityPathPattern = regexp.MustCompile(`(?i)(?:[a-z]:[\\/]|\.?\.?[\\/])?[^\s"'|<>]+\.(?:go|rs|py|js|jsx|ts|tsx|json|ya?ml|toml|md|css|scss|html?|sql|sh|ps1|bat|cmd|java|kt|swift|c|cc|cpp|h|hpp)`)

type activityDigest struct {
	Items      []activityCardItem
	Operations int
}

type aggregatedActivity struct {
	activityCardItem
	significant bool
}

func aggregateActivities(snapshot *appserver.ThreadReadSnapshot) activityDigest {
	if snapshot == nil {
		return activityDigest{}
	}
	tools := make([]model.DetailItem, 0, len(snapshot.DetailItems)+1)
	indexByKey := map[string]int{}
	for _, item := range snapshot.DetailItems {
		if item.Kind != model.DetailItemTool {
			continue
		}
		key := activityOperationKey(item)
		if index, exists := indexByKey[key]; exists {
			tools[index] = mergeActivityTool(tools[index], item)
			continue
		}
		indexByKey[key] = len(tools)
		tools = append(tools, item)
	}
	if label := strings.TrimSpace(snapshot.LatestToolLabel); label != "" {
		latest := model.DetailItem{
			ID:       strings.TrimSpace(snapshot.LatestToolID),
			Kind:     model.DetailItemTool,
			ToolKind: strings.TrimSpace(snapshot.LatestToolKind),
			Label:    label,
			Status:   strings.TrimSpace(snapshot.LatestToolStatus),
			FP:       strings.TrimSpace(snapshot.LatestToolFP),
		}
		key := activityOperationKey(latest)
		if index, exists := indexByKey[key]; exists {
			tools[index] = mergeActivityTool(tools[index], latest)
		} else {
			indexByKey[key] = len(tools)
			tools = append(tools, latest)
		}
	}

	all := make([]aggregatedActivity, 0, len(tools))
	currentIndex := -1
	for index, tool := range tools {
		current := activityToolIsCurrent(snapshot, tool, index == len(tools)-1)
		view := classifyActivity(tool.ToolKind, tool.Label, current)
		all = append(all, view)
		if current {
			currentIndex = index
		}
	}

	completedSlots := activityHistoryLimit
	if currentIndex >= 0 {
		completedSlots--
	}
	selected := selectCompletedActivities(all, currentIndex, completedSlots)
	if currentIndex >= 0 {
		selected = append(selected, all[currentIndex])
	}
	items := make([]activityCardItem, 0, len(selected))
	for _, item := range selected {
		items = append(items, item.activityCardItem)
	}
	return activityDigest{Items: items, Operations: len(tools)}
}

func selectCompletedActivities(items []aggregatedActivity, currentIndex, limit int) []aggregatedActivity {
	if limit <= 0 {
		return nil
	}
	important := make([]int, 0, limit)
	fallback := make([]int, 0, limit)
	for index, item := range items {
		if index == currentIndex || item.Current {
			continue
		}
		if item.significant {
			important = append(important, index)
		} else {
			fallback = append(fallback, index)
		}
	}
	if len(important) >= limit {
		selected := make([]aggregatedActivity, 0, limit)
		for _, index := range important[len(important)-limit:] {
			selected = append(selected, items[index])
		}
		return selected
	}
	need := limit - len(important)
	if need > len(fallback) {
		need = len(fallback)
	}
	selectedIndexes := make(map[int]struct{}, limit)
	for _, index := range fallback[len(fallback)-need:] {
		selectedIndexes[index] = struct{}{}
	}
	for _, index := range important {
		selectedIndexes[index] = struct{}{}
	}
	selected := make([]aggregatedActivity, 0, limit)
	for index, item := range items {
		if _, ok := selectedIndexes[index]; ok {
			selected = append(selected, item)
		}
	}
	return selected
}

func mergeActivityTool(previous, current model.DetailItem) model.DetailItem {
	if strings.TrimSpace(current.ToolKind) == "" {
		current.ToolKind = previous.ToolKind
	}
	if strings.TrimSpace(current.Label) == "" {
		current.Label = previous.Label
	}
	if strings.TrimSpace(current.Status) == "" {
		current.Status = previous.Status
	}
	if strings.TrimSpace(current.ID) == "" {
		current.ID = previous.ID
	}
	if strings.TrimSpace(current.FP) == "" {
		current.FP = previous.FP
	}
	return current
}

func activityOperationKey(item model.DetailItem) string {
	if id := strings.TrimSpace(item.ID); id != "" {
		return "id:" + id
	}
	if fp := strings.TrimSpace(item.FP); fp != "" {
		return "fp:" + fp
	}
	return "label:" + strings.ToLower(compactActivityCommand(item.Label, 300))
}

func activityToolIsCurrent(snapshot *appserver.ThreadReadSnapshot, item model.DetailItem, isLast bool) bool {
	if terminalToolStatus(item.Status) || isTerminalStatus(snapshot.LatestTurnStatus) {
		return false
	}
	latestID := strings.TrimSpace(snapshot.LatestToolID)
	itemID := strings.TrimSpace(item.ID)
	sameLatest := latestID != "" && itemID == latestID
	if !sameLatest && latestID == "" {
		sameLatest = compactActivityCommand(item.Label, 300) == compactActivityCommand(snapshot.LatestToolLabel, 300)
	}
	return sameLatest && (snapshot.LatestToolLiveCurrent || isLast)
}

func classifyActivity(toolKind, label string, current bool) aggregatedActivity {
	command := compactActivityCommand(activityCommand(label), 180)
	lower := strings.ToLower(command)
	kind := strings.ToLower(strings.TrimSpace(toolKind))
	item := aggregatedActivity{activityCardItem: activityCardItem{Icon: "⚙️", Text: "执行命令", Command: command, Current: current}}

	switch {
	case kind == "filechange" || containsAny(lower, "apply_patch", "write-file", "set-content", "add-content"):
		item.Icon = "✏️"
		item.Text = activityWithPath("修改", command)
		item.significant = true
	case activityIsTest(lower):
		item.Icon = "🧪"
		item.Text = "运行测试"
		item.significant = true
	case activityIsFormat(lower):
		item.Icon = "✨"
		item.Text = "格式化代码"
		item.significant = true
	case activityIsGitInspection(lower):
		item.Icon = "🔍"
		item.Text = "检查变更"
		item.significant = true
	case activityIsSearch(kind, lower):
		item.Icon = "🔍"
		item.Text = activitySearchText(command)
	case activityIsRead(lower):
		item.Icon = "📄"
		item.Text = activityWithPath("查看", command)
	case kind == "websearch":
		item.Icon = "🌐"
		item.Text = "搜索资料"
	case containsAny(lower, "go build", "npm run build", "pnpm build", "yarn build", "cargo build", "dotnet build"):
		item.Icon = "🔨"
		item.Text = "构建项目"
		item.significant = true
	}
	return item
}

func activityCommand(label string) string {
	label = strings.TrimSpace(cleanTelegramNilLiteral(label))
	if parsed := parseShellTool(label); parsed.Command != "" {
		return strings.Trim(parsed.Command, `"`)
	}
	return label
}

func compactActivityCommand(command string, limit int) string {
	command = strings.Join(strings.Fields(strings.ReplaceAll(command, "\x00", "")), " ")
	command = strings.TrimSpace(command)
	if limit <= 0 {
		return command
	}
	runes := []rune(command)
	if len(runes) <= limit {
		return command
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func activityWithPath(verb, command string) string {
	if path := activityFilePath(command); path != "" {
		return verb + " " + path
	}
	if verb == "查看" {
		return "查看文件"
	}
	return "修改文件"
}

func activityFilePath(command string) string {
	match := activityPathPattern.FindString(command)
	match = strings.Trim(match, `.,;:()[]{}"'`)
	if match == "" {
		return ""
	}
	match = filepath.ToSlash(match)
	if len([]rune(match)) > 70 {
		return "…/" + filepath.Base(match)
	}
	return match
}

func activitySearchText(command string) string {
	fields := strings.Fields(command)
	for index, field := range fields {
		base := strings.ToLower(filepath.Base(strings.Trim(field, `"'`)))
		if base != "rg" && base != "rg.exe" && base != "grep" && base != "grep.exe" {
			continue
		}
		for _, candidate := range fields[index+1:] {
			candidate = strings.Trim(candidate, `"'`)
			if candidate == "" || strings.HasPrefix(candidate, "-") || activityFilePath(candidate) != "" {
				continue
			}
			if activitySimpleQuery(candidate) {
				return "搜索 " + candidate + " 相关实现"
			}
			break
		}
	}
	return "搜索代码"
}

func activitySimpleQuery(value string) bool {
	if len([]rune(value)) > 40 {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return value != ""
}

func activityIsTest(command string) bool {
	return containsAny(command,
		"go test", "npm test", "npm run test", "pnpm test", "yarn test", "cargo test", "pytest", "dotnet test", "swift test", "gradle test", "mvn test")
}

func activityIsFormat(command string) bool {
	return containsAny(command, "gofmt ", "go fmt", "prettier ", "eslint --fix", "rustfmt", "cargo fmt", "black ", "ruff format")
}

func activityIsGitInspection(command string) bool {
	return containsAny(command, "git diff", "git status", "git show", "git log")
}

func activityIsSearch(kind, command string) bool {
	if kind == "websearch" {
		return false
	}
	return containsAny(command, "rg ", "rg.exe ", "grep ", "grep.exe ", "git grep ", "select-string ")
}

func activityIsRead(command string) bool {
	trimmed := strings.TrimSpace(command)
	return strings.HasPrefix(trimmed, "cat ") || strings.HasPrefix(trimmed, "type ") || strings.HasPrefix(trimmed, "read ") ||
		strings.HasPrefix(trimmed, "get-content ") || strings.HasPrefix(trimmed, "gc ") || strings.HasPrefix(trimmed, "sed -n ")
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
