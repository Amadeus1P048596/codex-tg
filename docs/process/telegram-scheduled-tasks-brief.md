# Telegram Scheduled Tasks Bridge Brief

## Problem

Codex Desktop injects its private `automation_update` tool into Desktop-hosted
threads. Telegram uses a separate App Server and `CODEX_HOME`, so its threads do
not receive that tool and cannot create native Scheduled tasks.

## Goal

A Telegram Codex thread can create, view, update, list, and delete standalone
Codex Desktop Scheduled tasks without sharing Desktop sessions or adding a
second scheduler.

## Non-goals

- Sharing Desktop thread history, runtime SQLite, writer locks, or caches.
- Creating heartbeat tasks attached to a Telegram thread.
- Executing schedules or mirroring Scheduled run output into Telegram.
- Exposing an App Server or MCP listener on the network.

## Operator Flow

The operator configures `CTR_GO_AUTOMATIONS_DIR`, normally pointing at the
Desktop `~/.codex/automations` directory, and asks Codex in Telegram for a
recurring standalone task. The injected tool writes a native cron definition.
Codex Desktop remains responsible for execution, history, UI, and notifications.

## Architecture

`internal/automation` owns a narrow native-store adapter and stdio MCP server.
`internal/appserver` injects that MCP server through thread-scoped config on
both `thread/start` and `thread/resume`. Existing configs stay disabled until
the directory is explicitly set.

## Testing

- Store CRUD preserves unknown native TOML fields and validates safe ids/RRULEs.
- MCP handshake, tool discovery, success, and error responses are covered.
- App Server config injection and private config loading are covered.
- Full Go tests/build, direct stdio MCP smoke, native-store smoke, and a targeted
  private-data scan are required before release.

## Acceptance Criteria

- [x] New and resumed Telegram threads receive `automation_update` when enabled.
- [x] Created tasks use native cron fields and a local execution environment.
- [x] Heartbeat tasks and unsafe paths are rejected.
- [x] Disabling the directory removes the tool and Desktop write boundary.
