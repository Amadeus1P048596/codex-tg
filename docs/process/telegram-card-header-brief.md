# Telegram Card Header Brief

## Problem

Observer cards render project, thread, short ids, role, status, and timing as
similar-weight bracketed text. Operators cannot quickly distinguish the thread,
speaker, result, and duration from routing metadata.

## Goal

Make the thread title, role, terminal/active status, and runtime the primary
visual hierarchy while preserving the project and short `T:` / `R:` identity
chips required for routing orientation.

## Non-goals

- Do not change message routing, callbacks, panel lifecycle, notification policy,
  or Telegram message ordering.
- Do not remove project, thread, turn, or kind identity.
- Do not introduce a locale or translation system.

## UX / Operator Flow

Rendered Markdown cards use three compact header rows:

1. marker plus bold thread title;
2. bold role, status, and timing when available;
3. code-styled project and short `T:` / `R:` metadata.

The request or response body remains below a blank separator. Telegram entities,
not raw HTML or Markdown delimiters, provide the styling.

## Domain Model

No persisted model changes. The existing visual identity inputs are rendered
through a structured card-header view.

## Architecture

Keep marker assignment and visual identity in `internal/daemon/visual_identity.go`.
Apply the structured header to rendered User, commentary, Plan, Final, and Details
cards through the existing `internal/tgformat` entity pipeline.

## Testing

- Unit-test header text and UTF-16 Telegram entity ranges.
- Regression-test User and Final cards for emphasized role/status/timing.
- Run `go test ./...` and `go build -buildvcs=false ./...`.
- Rebuild/restart the local daemon and inspect real Telegram readback.

## Acceptance Criteria

- [x] The thread title is the only primary item on the first row.
- [x] User and assistant roles are visually emphasized.
- [x] Final status and duration are visible together before the response body.
- [x] Project plus short thread/turn ids remain present but visually secondary.
- [x] Existing Markdown body entities and callback behavior remain intact.

## Validation Result

- 2026-08-20 CST: rebuilt and restarted the local Windows daemon, then confirmed
  the structured header through operator-provided Telegram client readback. The
  thread title, commentary role, status, duration, secondary project/thread/turn
  metadata, and Markdown body hierarchy rendered as intended.
