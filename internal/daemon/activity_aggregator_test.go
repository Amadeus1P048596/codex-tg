package daemon

import (
	"testing"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestAggregateActivitiesDeduplicatesAndKeepsRecentMeaningfulSteps(t *testing.T) {
	t.Parallel()

	snapshot := &appserver.ThreadReadSnapshot{
		LatestToolID:          "test-1",
		LatestToolKind:        "commandExecution",
		LatestToolLabel:       `powershell.exe -Command "go test ./internal/daemon/..."`,
		LatestToolStatus:      "running",
		LatestToolLiveCurrent: true,
		DetailItems: []model.DetailItem{
			{ID: "search-1", Kind: model.DetailItemTool, ToolKind: "commandExecution", Label: `powershell.exe -Command "rg -n trimOutputTail internal/daemon"`, Status: "completed"},
			{ID: "read-1", Kind: model.DetailItemTool, ToolKind: "commandExecution", Label: `powershell.exe -Command "Get-Content internal/daemon/output.go"`, Status: "completed"},
			{ID: "edit-1", Kind: model.DetailItemTool, ToolKind: "fileChange", Label: `internal/daemon/output.go`, Status: "completed"},
			{ID: "format-1", Kind: model.DetailItemTool, ToolKind: "commandExecution", Label: `gofmt -w internal/daemon/output.go`, Status: "completed"},
			{ID: "test-1", Kind: model.DetailItemTool, ToolKind: "commandExecution", Label: `powershell.exe -Command "go test ./internal/daemon/..."`, Status: "running"},
			// The live overlay may repeat the same tool ID; it is one operation.
			{ID: "test-1", Kind: model.DetailItemTool, ToolKind: "commandExecution", Label: `powershell.exe -Command "go test ./internal/daemon/..."`, Status: "running"},
		},
	}

	digest := aggregateActivities(snapshot)
	if digest.Operations != 5 {
		t.Fatalf("Operations = %d, want 5", digest.Operations)
	}
	if len(digest.Items) != 3 {
		t.Fatalf("Items = %#v, want three", digest.Items)
	}
	if digest.Items[0].Text != "修改 internal/daemon/output.go" || digest.Items[0].Current {
		t.Fatalf("first item = %#v", digest.Items[0])
	}
	if digest.Items[1].Text != "格式化代码" || digest.Items[1].Current {
		t.Fatalf("second item = %#v", digest.Items[1])
	}
	if digest.Items[2].Text != "运行测试" || !digest.Items[2].Current || digest.Items[2].Icon != "🧪" {
		t.Fatalf("current item = %#v", digest.Items[2])
	}
	if digest.Items[2].Command != "go test ./internal/daemon/..." {
		t.Fatalf("current command = %q", digest.Items[2].Command)
	}
}

func TestAggregateActivitiesUsesQuickReadsOnlyAsFallback(t *testing.T) {
	t.Parallel()

	digest := aggregateActivities(&appserver.ThreadReadSnapshot{DetailItems: []model.DetailItem{
		{ID: "search", Kind: model.DetailItemTool, Label: `rg -n output internal/daemon`, Status: "completed"},
		{ID: "read", Kind: model.DetailItemTool, Label: `Get-Content internal/daemon/output.go`, Status: "completed"},
	}})
	if digest.Operations != 2 || len(digest.Items) != 2 {
		t.Fatalf("digest = %#v", digest)
	}
	if digest.Items[0].Text != "搜索 output 相关实现" {
		t.Fatalf("search = %#v", digest.Items[0])
	}
	if digest.Items[1].Text != "查看 internal/daemon/output.go" {
		t.Fatalf("read = %#v", digest.Items[1])
	}
}

func TestAggregateActivitiesKeepsSelectedStepsInExecutionOrder(t *testing.T) {
	t.Parallel()

	digest := aggregateActivities(&appserver.ThreadReadSnapshot{DetailItems: []model.DetailItem{
		{ID: "edit", Kind: model.DetailItemTool, ToolKind: "fileChange", Label: `internal/daemon/output.go`, Status: "completed"},
		{ID: "read", Kind: model.DetailItemTool, Label: `Get-Content internal/daemon/output.go`, Status: "completed"},
		{ID: "format", Kind: model.DetailItemTool, Label: `gofmt -w internal/daemon/output.go`, Status: "completed"},
	}})
	if len(digest.Items) != 3 {
		t.Fatalf("Items = %#v, want three", digest.Items)
	}
	if digest.Items[0].Text != "修改 internal/daemon/output.go" || digest.Items[1].Text != "查看 internal/daemon/output.go" || digest.Items[2].Text != "格式化代码" {
		t.Fatalf("Items = %#v, want chronological order", digest.Items)
	}
}

func TestClassifyActivityFallsBackWithoutLeakingInternalEventLabels(t *testing.T) {
	t.Parallel()

	item := classifyActivity("dynamicToolCall", "custom_tool --opaque", false)
	if item.Text != "执行命令" || item.Icon != "⚙️" {
		t.Fatalf("item = %#v", item)
	}
	for _, forbidden := range []string{"[Tool]", "[Output]", "Last completed tool", "Status:"} {
		if item.Text == forbidden {
			t.Fatalf("internal event label leaked: %q", item.Text)
		}
	}
}
