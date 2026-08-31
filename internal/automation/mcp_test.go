package automation

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestServeMCPListsAndCallsAutomationTool(t *testing.T) {
	t.Parallel()

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		``,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"automation_update","arguments":{"mode":"create","name":"Morning check","prompt":"Check the project.","rrule":"FREQ=DAILY;BYHOUR=8;BYMINUTE=0","status":"ACTIVE","kind":"cron","projectId":null,"model":"gpt-test","reasoningEffort":"medium","executionEnvironment":"local"}}}`,
	}
	var out bytes.Buffer
	store := NewStore(t.TempDir(), func() time.Time {
		return time.Date(2026, time.August, 31, 8, 0, 0, 0, time.Local)
	})
	if err := ServeMCP(strings.NewReader(strings.Join(requests, "\n")+"\n"), &out, store); err != nil {
		t.Fatalf("ServeMCP failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("response count = %d, want 3:\n%s", len(lines), out.String())
	}
	var initialize, list, call map[string]any
	for i, target := range []*map[string]any{&initialize, &list, &call} {
		if err := json.Unmarshal([]byte(lines[i]), target); err != nil {
			t.Fatalf("response %d is invalid JSON: %v", i, err)
		}
	}
	if result := initialize["result"].(map[string]any); result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize result = %#v", result)
	}
	listResult := list["result"].(map[string]any)
	tools := listResult["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "automation_update" {
		t.Fatalf("tools = %#v", tools)
	}
	schema := tools[0].(map[string]any)["inputSchema"].(map[string]any)
	if branches, ok := schema["oneOf"].([]any); !ok || len(branches) != 5 {
		t.Fatalf("input schema branches = %#v, want 5 operation variants", schema["oneOf"])
	}
	callResult := call["result"].(map[string]any)
	if callResult["isError"] != false {
		t.Fatalf("call result = %#v", callResult)
	}
	content := callResult["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("tool text is invalid JSON: %v", err)
	}
	if payload["id"] == "" || payload["status"] != "ACTIVE" {
		t.Fatalf("tool payload = %#v", payload)
	}
}

func TestServeMCPReturnsToolErrorsAsMCPContent(t *testing.T) {
	t.Parallel()

	request := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"automation_update","arguments":{"mode":"view","id":"../escape"}}}` + "\n"
	var out bytes.Buffer
	if err := ServeMCP(strings.NewReader(request), &out, NewStore(t.TempDir(), time.Now)); err != nil {
		t.Fatalf("ServeMCP failed: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &response); err != nil {
		t.Fatalf("response is invalid JSON: %v", err)
	}
	result := response["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("result = %#v, want isError=true", result)
	}
}
