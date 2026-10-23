# Telegram Compact Navigation And Final Card Brief

Status: implemented and automatically validated; live Telegram validation is
required before deployment.

## Problem

Telegram-only operators see duplicate session-creation choices and long command
names for the three most common navigation actions. Completed activity cards
also split a final answer by line count, so the visible body contains the first
part while the expandable block contains only the remainder. That makes neither
surface read like a deliberate summary or a complete answer.

## Goal

- Make `/h`, `/new`, `/t`, `/c`, `/a`, and `/un` the primary Telegram commands
  for Home, a new isolated Chat, thread selection, the current session,
  archiving, and restoring an archived session.
- Keep the existing long commands routeable for compatibility, but remove
  `/newchat` and `/newthread` from the primary command menu.
- Make `/new` use the dated, isolated Chat-folder behavior previously exposed
  by `/newchat`; project-scoped creation remains available through `/projects`.
- For a multi-paragraph final answer that fits one activity card, show a compact
  first-paragraph summary outside and the complete answer inside the expandable
  block.
- Reduce running-card actions to `停止`, `追加指令`, and `切换会话`; reduce
  completed-card actions to `详情` and `切换会话`.

## Non-goals

- Removing compatibility routes for `/home`, `/threads`, `/newchat`, or
  `/newthread`.
- Changing App Server thread identity, title collection, image input, or project
  selection behavior.
- Changing Details pagination or Tools file behavior.
- Removing legacy callback handlers or diagnostic commands solely because their
  buttons are no longer rendered.
- Forcing a long final answer into one Telegram message; long results continue
  in separate `Codex · 结果` messages.

## UX / Operator Flow

- `/h` opens Home; `/home` remains an alias.
- `/t` opens thread selection; `/threads` remains an alias.
- `/c`, `/a`, and `/un` expose current-session, archive, and restore actions;
  `/current`, `/archive`, and `/unarchive` remain compatibility aliases.
- `/new` starts the existing title-then-prompt Chat flow and creates a dated cwd
  under the configured Chats root. `/new <prompt>` keeps the one-line shortcut.
- The Home `新建会话` action starts the same `/new` flow directly instead of
  asking the operator to choose between Chat and ordinary thread types.
- A completed card shows a compact summary in the normal body. Expanding the
  inline `详情` block reveals the complete final answer for that turn when it
  fits the compact-card budget.
- `切换会话` opens the thread picker in a new Telegram response so the activity
  card remains intact. The picker uses five-session cursor pages with previous
  and next navigation. Internal and scheduled-task execution threads are
  filtered before counting the five visible sessions, so raw App Server pages
  cannot produce sparse picker pages. Writer management, card refresh, context,
  full-log, and full-id diagnostics are not shown as activity-card buttons.
- Switching away from a session whose terminal result was already visible does
  not replay that same result as a background completion or inbox reminder;
  work that was still running continues to notify when it actually finishes.

## Architecture

Command aliases stay in `internal/daemon` routing, while the public Telegram
command list stays in `internal/telegram`. Activity-card actions and Final-card
summary extraction stay in `internal/daemon`; the generic notification renderer
keeps its existing behavior for running, input, and non-final cards.

## Testing

- Menu tests prove the compact commands are exposed and legacy creation commands
  are hidden rather than removed.
- Routing tests prove `/h` and `/t` match their long aliases and `/new` creates a
  Chat cwd.
- Final-card tests prove the visible body is the first-paragraph summary and the
  expandable block contains the complete answer.
- Action-layout tests assert the exact running and completed button sets and
  prove the thread picker does not overwrite its source activity card.
- Navigation tests cover cursor pagination and filling a visible page across
  filtered raw pages; foreground-session tests cover terminal acknowledgement
  without suppressing a later real completion.
- Run targeted tests, `go test ./...`, `go build -buildvcs=false ./...`, and
  `git diff --check` before deployment.

## Acceptance Criteria

- [x] `/h`, `/new`, `/t`, `/c`, `/a`, and `/un` are available from Telegram's
  command menu.
- [x] `/new` and the Home new-session action use isolated Chat-folder creation.
- [x] Existing long command routes remain compatible.
- [x] Multi-paragraph compact finals show summary outside and the complete answer
  inside the expandable block.
- [x] Running and completed activity cards expose only their compact action sets.
- [x] `切换会话` opens a picker without editing the activity card.
- [x] The picker exposes older runtime sessions through previous/next pages.
- [x] Each picker page is filled with up to five visible sessions after internal
  and scheduled-task execution sessions are filtered.
- [x] Switching away from an already completed foreground session does not emit
  a duplicate background completion reminder.
- [x] No daemon restart occurs without explicit operator approval.
