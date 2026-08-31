# codex-tg Unified Push Service Brief

## Problem

Telegram-native scheduled runs currently enter the normal observer/session-card
flow. That makes a background schedule look like a newly opened conversation,
while external alerts such as the bank-gold monitor send a compact standalone
notification directly to Telegram. The two paths also duplicate delivery,
routing, authentication, retry, and message-shaping concerns.

Scheduled task definitions do not currently retain the Telegram-private Codex
thread that created them, so a terminal notification cannot explain which
conversation owns the task.

## Goal

- Provide one codex-tg push service for internal and authenticated external
  producers.
- Deliver Telegram-native scheduled results as standalone notifications rather
  than normal observer/session cards.
- Include the owning conversation title on every scheduled-task notification.
- Move the bank-gold alert from direct Telegram Bot API access to the same push
  service, so the NAS no longer needs the Telegram bot token.

## Non-goals

- Reusing a prior mutable Codex turn for scheduled execution.
- Sharing Desktop and Telegram App Server sessions, task stores, or histories.
- Making the push listener public by default.
- Supporting arbitrary Telegram formatting, buttons, files, or destinations in
  the first slice.
- Replacing Codex App Server as the authority for scheduled thread/turn state.

## User-visible behavior

- A due task still runs in a fresh internal thread in the Telegram-private App
  Server, preserving isolated execution state.
- That internal thread does not create a Working/Final conversation card or an
  inbox item. On terminal completion, the push service sends one compact
  notification containing the task name, owning conversation title, state, and
  final result or failure detail.
- `failed_runs_only` continues to suppress successful notifications.
- The bank-gold monitor publishes its existing message body through the same
  service under the source label `bank-gold`.

## Ownership

The daemon captures a successful `automation_update` `mcpToolCall` completion
from the authoritative App Server event. It resolves the event's `threadId` to
the locally stored user-facing title and persists `source_thread_id` plus
`source_thread_title` in the task definition without changing the scheduling
`updated_at` boundary. Existing definitions can be migrated with the same store
operation.

## Service boundary

The core accepts a typed request containing source, title, body, optional
conversation title, severity, and idempotency key. Internal producers call it
directly. An optional HTTP adapter accepts `POST /v1/push` for external
producers.

The HTTP listener is disabled unless explicitly configured. When enabled it:

- requires a separate push token and constant-time Bearer validation;
- accepts only JSON with a bounded body and bounded text fields;
- does not accept a Telegram chat id, Bot Token, parse mode, URL, or file path;
- routes through the configured codex-tg observer target;
- returns a duplicate-safe success for a repeated idempotency key;
- logs metadata only, never the request body or authorization value.

## Tests

- Store origin updates preserve unknown fields and `updated_at`.
- Completed App Server MCP events attach the real source thread/title.
- Scheduled terminal snapshots publish exactly once and do not create observer
  panels or inbox items.
- `failed_runs_only` suppresses a successful scheduled notification.
- The HTTP adapter rejects missing/incorrect authorization and oversized or
  invalid requests, and accepts a valid request once.
- Bank-gold tests prove the generic request contract and that Telegram
  credentials are no longer required when push-service credentials exist.

## Acceptance

- Targeted tests, full Go tests/build, Python tests/compile, diff check, and
  private-data scan pass.
- A staged daemon plus staged NAS container configuration are ready without
  changing the running daemon or alert container.
- After operator-approved deployment, one real external test and one real due
  task are read back from Telegram and correlated with sanitized logs.
