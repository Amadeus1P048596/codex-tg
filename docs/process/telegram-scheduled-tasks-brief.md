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
thread/turn in the TG-private App Server, and keeps that execution thread out of
normal observer cards, `/home`, and `/inbox`. When the run completes, fails, is
cancelled, or needs input, the unified push service sends one standalone result
that names the task's owning Telegram conversation.

## Architecture

`internal/automation` owns the safe definition store and RRULE calculation.
`internal/appserver` injects the task-management MCP server into new and resumed
Telegram threads. `internal/daemon` owns polling, durable slot claims, new
thread/turn dispatch, App Server completion-event origin capture, and unified
push delivery. App Server remains authoritative for the scheduled thread after
dispatch.

## Testing

- Store CRUD preserves unknown fields, marks new tasks as Telegram-owned, and
  validates safe ids/RRULEs.
- RRULE tests cover local wall clock, update boundaries, hourly and weekly
  intervals, monthly dates and intervals, leap days, and missing calendar days.
- Daemon regression proves one due slot starts exactly one unbound background
  thread with the task prompt and shared runtime model/reasoning settings.
- Push regressions prove completed MCP events attach task origin without moving
  the schedule boundary, and scheduled terminal snapshots create exactly one
  push but no normal conversation panel. An exact successful `DONT_NOTIFY`
  result is recorded without a push.
- MCP handshake, tool discovery, config injection, full Go tests/build, and a
  targeted private-data scan remain required.
- Live Telegram completion readback is required after an operator-approved
  daemon restart.

## Acceptance Criteria

- [x] New and resumed Telegram threads receive `automation_update` when enabled.
- [x] New tasks live in the TG-private store and carry an ownership marker.
- [x] Heartbeat tasks and unsafe paths are rejected.
- [x] Due occurrences are durably claimed and dispatched once.
- [x] Monthly RRULEs support `BYMONTHDAY=1..31` and skip months where a selected
  date does not exist.
- [x] Scheduled runs use new TG-private App Server threads and never change the
  current Telegram binding.
- [x] Scheduled runs inherit the same `/model` and `/effort` settings as all
  conversation turns; task definitions carry no independent execution model.
- [x] New task definitions retain their owning source thread and title.
- [x] Scheduled terminal results use the unified push path and do not create
  normal observer, Home, or inbox entries.
- [x] Exact successful `DONT_NOTIFY` results complete silently.
- [ ] A real due task completes and produces Telegram readback after the pending
  operator-approved deployment.
