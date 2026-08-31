# Telegram-Native Scheduled Tasks Brief

## Problem

The original bridge wrote task files into the Codex Desktop automation store.
That bypassed Desktop's host-owned registration lifecycle, so a definition could
exist without ever becoming runnable. Desktop-owned execution also had no
authoritative path back to the isolated Telegram runtime or its observer.

## Goal

A Telegram Codex thread can create, view, update, list, delete, execute, and
receive results for standalone scheduled tasks without depending on or sharing
mutable state with Codex Desktop.

## Non-goals

- Sharing Desktop thread history, task definitions, SQLite, writer locks, or caches.
- Creating heartbeat tasks attached to an existing thread.
- Mirroring Desktop Scheduled history or notifications.
- Exposing an App Server or MCP listener on the network.

## Operator Flow

The operator asks Codex in Telegram for a recurring task. The injected MCP tool
writes a validated cron definition under `~/.codex-tg/automations` by default.
The daemon claims each due local-time slot in SQLite, starts a new background
thread/turn in the TG-private App Server, and sends lifecycle/result visibility
through the normal observer, `/home`, and `/inbox` behavior.

## Architecture

`internal/automation` owns the safe definition store and RRULE calculation.
`internal/appserver` injects the task-management MCP server into new and resumed
Telegram threads. `internal/daemon` owns polling, durable slot claims, new
thread/turn dispatch, and failure notification. App Server remains authoritative
for the scheduled thread after dispatch.

## Testing

- Store CRUD preserves unknown fields, marks new tasks as Telegram-owned, and
  validates safe ids/RRULEs.
- RRULE tests cover local wall clock, update boundaries, hourly intervals, and
  weekly intervals.
- Daemon regression proves one due slot starts exactly one unbound background
  thread with the task prompt/model/reasoning settings.
- MCP handshake, tool discovery, config injection, full Go tests/build, and a
  targeted private-data scan remain required.
- Live Telegram completion readback is required after an operator-approved
  daemon restart.

## Acceptance Criteria

- [x] New and resumed Telegram threads receive `automation_update` when enabled.
- [x] New tasks live in the TG-private store and carry an ownership marker.
- [x] Heartbeat tasks and unsafe paths are rejected.
- [x] Due occurrences are durably claimed and dispatched once.
- [x] Scheduled runs use new TG-private App Server threads and never change the
  current Telegram binding.
- [ ] A real due task completes and produces Telegram readback after the pending
  operator-approved deployment.
