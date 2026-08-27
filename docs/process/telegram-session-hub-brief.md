# Telegram Session Hub Brief

## Problem

Telegram now has stable per-turn cards and one foreground session, but session
management is still spread across commands. Background completion notices are
ephemeral, archiving leaves a dead end, manual titles can be overwritten by a
later runtime refresh, and primary cards mix Chinese and English product terms.

## Goal

Provide one compact session home, a durable background-attention inbox, safe
archive and restore flows, user-owned manual titles, and consistent Chinese
copy on the primary Telegram surfaces.

## Non-goals

- Notification preference controls or digest scheduling.
- An isolation explainer.
- A verbose/debug mode or removal of existing diagnostic exports.

## UX / Operator Flow

- `/home` shows the current session title, state, elapsed time when relevant,
  and the number of background sessions needing attention.
- Home buttons open the current card, the available-session list, a new-session
  chooser, or `/inbox` in place.
- `/inbox` lists durable background Completed, Failed, Cancelled, and Needs
  input items as title buttons. Switching to an item clears it.
- `/archive` refuses to archive a running or input-blocked session. An idle
  session still requires confirmation, then lands on actions for switching or
  creating another session.
- `/unarchive` restore success offers both `切换至该会话` and
  `继续查看归档`.
- `/title <name>` records a user-owned title. Runtime refreshes cannot replace
  that title.
- Primary status labels and action buttons use Chinese product language.

## Domain Model

- `ui.inbox.<chat-topic>.<thread>` stores one durable attention item.
- `ui.thread_title.manual.<thread>` stores the current user-owned title.
- New callbacks: `home_overview`, `home_threads`, `home_inbox`,
  `home_show_current`, `home_new_menu`, `home_new_chat`, and
  `home_new_thread`.

## Architecture

`internal/daemon/thread_navigation.go` owns home and inbox rendering plus their
SQLite-backed state. Existing foreground routing remains authoritative for the
selected session. App Server remains authoritative for live thread state and
archive operations.

## Testing

- Unit tests for home, inbox persistence, switch clearing, manual-title
  ownership, active archive protection, archive landing, and restore actions.
- Existing activity-card, foreground-session, callback, and command-menu
  regression suites.
- Full `go test ./...`, build, formatting, and diff checks.
- Live Telegram readback after an idle-gated cutover.

## Acceptance Criteria

- [x] `/home` is a useful single entry point and exposes the inbox count.
- [x] Background attention survives restart and clears on switch.
- [x] Active or input-blocked sessions cannot be archived.
- [x] Archive and restore flows always expose a useful next action.
- [x] Manual titles survive runtime refresh.
- [x] Primary Telegram cards and buttons use consistent Chinese labels.
