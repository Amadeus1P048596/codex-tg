package automation

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const toolDescription = "Manage Scheduled tasks owned and executed by codex-tg. Use this for recurring tasks, reminders, monitoring, or scheduled follow-ups requested from Telegram. Telegram and Codex Desktop use isolated task stores and sessions, so this integration creates standalone cron tasks only; heartbeat tasks cannot target Telegram threads. Each due run starts a new background thread in the Telegram-private Codex runtime and sends its lifecycle/result through the Telegram observer. Prefer updating an existing task over creating a duplicate. Times are local wall-clock times. Never put credentials or secrets in an automation prompt."

func ServeMCP(in io.Reader, out io.Writer, store *Store) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var request map[string]any
		if err := json.Unmarshal(line, &request); err != nil {
			if err := encoder.Encode(rpcError(nil, -32700, "parse error")); err != nil {
				return err
			}
			continue
		}
		id, hasID := request["id"]
		method, _ := request["method"].(string)
		if !hasID {
			continue
		}
		var response map[string]any
		switch method {
		case "initialize":
			params := mapValue(request["params"])
			protocolVersion, _ := params["protocolVersion"].(string)
			if strings.TrimSpace(protocolVersion) == "" {
				protocolVersion = "2025-06-18"
			}
			response = rpcResult(id, map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
				"serverInfo":      map[string]any{"name": "codex-tg-automations", "version": "1"},
			})
		case "ping":
			response = rpcResult(id, map[string]any{})
		case "tools/list":
			response = rpcResult(id, map[string]any{"tools": []any{automationToolSpec()}})
		case "tools/call":
			params := mapValue(request["params"])
			name, _ := params["name"].(string)
			if name != "automation_update" {
				response = rpcResult(id, toolError(fmt.Errorf("unknown tool %q", name)))
				break
			}
			result, err := store.Apply(mapValue(params["arguments"]))
			if err != nil {
				response = rpcResult(id, toolError(err))
				break
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				response = rpcResult(id, toolError(err))
				break
			}
			response = rpcResult(id, map[string]any{
				"content": []any{map[string]any{"type": "text", "text": string(encoded)}},
				"isError": false,
			})
		default:
			response = rpcError(id, -32601, "method not found")
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func automationToolSpec() map[string]any {
	commonProperties := map[string]any{
		"mode": map[string]any{
			"type": "string", "enum": []string{"list", "view", "create", "update", "delete"},
		},
		"id": map[string]any{
			"type": "string", "description": "Automation id. Required for view, update, and delete.",
		},
		"name": map[string]any{
			"type": "string", "description": "Short human-readable automation name.",
		},
		"prompt": map[string]any{
			"type": "string", "description": "Self-contained task prompt only. Do not include the schedule or credentials.",
		},
		"rrule": map[string]any{
			"type": "string", "description": "Local-time RRULE without DTSTART. Supported FREQ values are HOURLY, DAILY, and WEEKLY.",
		},
		"status": map[string]any{
			"type": "string", "enum": []string{"ACTIVE", "PAUSED"},
		},
		"kind": map[string]any{
			"type": "string", "const": "cron", "description": "Telegram scheduling uses standalone cron tasks because sessions are isolated from Desktop.",
		},
		"cwd": map[string]any{
			"type": "string", "minLength": 1, "description": "Optional working directory for the isolated Telegram run. Omit it to use the daemon default working directory.",
		},
		"projectId": map[string]any{
			"anyOf":       []any{map[string]any{"type": "string", "minLength": 1}, map[string]any{"type": "null"}},
			"description": "Compatibility field. Use null unless migrating an older task; cwd controls the Telegram run directory.",
		},
		"model": map[string]any{
			"type": "string", "description": "Model for scheduled runs. Omit on update to preserve the existing model.",
		},
		"reasoningEffort": map[string]any{
			"type": "string", "enum": []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"},
		},
		"notificationPolicy": map[string]any{
			"anyOf":       []any{map[string]any{"type": "string", "const": "failed_runs_only"}, map[string]any{"type": "null"}},
			"description": "Use failed_runs_only to mute successful-run notifications; null removes that override.",
		},
		"executionEnvironment": map[string]any{
			"type": "string", "const": "local",
		},
	}
	branch := func(mode string, required ...string) map[string]any {
		properties := make(map[string]any, len(commonProperties))
		for key, value := range commonProperties {
			properties[key] = value
		}
		properties["mode"] = map[string]any{"type": "string", "const": mode}
		return map[string]any{
			"type":                 "object",
			"properties":           properties,
			"required":             append([]string{"mode"}, required...),
			"additionalProperties": false,
		}
	}
	return map[string]any{
		"name":        "automation_update",
		"description": toolDescription,
		"inputSchema": map[string]any{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"oneOf": []any{
				branch("list"),
				branch("view", "id"),
				branch("create", "name", "prompt", "rrule", "status", "kind", "projectId", "model", "reasoningEffort", "executionEnvironment"),
				branch("update", "id"),
				branch("delete", "id"),
			},
		},
	}
}

func toolError(err error) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": err.Error()}},
		"isError": true,
	}
}

func rpcResult(id any, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	}
}

func mapValue(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}
