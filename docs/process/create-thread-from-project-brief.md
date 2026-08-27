# Create Thread From Project Brief

## Problem

Operators can browse cached threads and projects from Telegram, but cannot start a brand-new Codex thread from a selected project/work directory.

## Goal

Telegram supports a project-first flow: `/projects` -> project menu -> `New thread` -> next message becomes the first prompt for a new thread in that project's existing `cwd`.

## Non-goals

- Creating, editing, or discovering new work directories.
- Accepting arbitrary filesystem paths from Telegram.
- Changing the App Server protocol.
- Creating empty threads without a first prompt.

## UX / Operator Flow

`/projects` renders project/workspace buttons grouped by normalized `cwd`. Codex UI Chats under generic `Documents/Codex` paths are shown under the separate `Chats` navigation instead of as normal projects. A project menu shows the workspace cwd, cached thread count, and actions: `New thread`, `Threads`, and `Bind latest`.

`New thread` arms a one-shot state for the current chat/topic. The next plain-text message calls App Server `thread/start` for that project cwd and then `turn/start` with the message text.

`/newchat` and `/newthread` without arguments collect two separate plain-text messages: an explicit title, then the first prompt. Either stage may be cancelled with `/cancel`; the state survives a daemon restart, expires after 15 minutes, and is isolated by Telegram chat/topic. The title is written to App Server and retained as user-owned Telegram metadata. The one-line `/newchat <prompt>` and `/newthread <prompt>` forms remain supported for backward compatibility and keep their prompt-derived title fallback. Interactive `/newchat` creates a dated Codex UI Chat cwd from the explicit title under the configured Chats root before calling App Server `thread/start`; `/newthread` calls `thread/start` without a Telegram-selected cwd. App Server may still attach its default cwd to the latter.

## Domain Model

Project/workspace selection is derived from cached `threads.cwd` data. One-shot create-thread state is stored in SQLite daemon state and expires quickly. New thread identity remains App Server-owned.

## Architecture

The daemon keeps App Server integration on stdio only:

- `thread/start` creates the thread in the selected cwd.
- `turn/start` starts the first run.
- SQLite stores the resulting thread, chat binding, panel state, callback routes, and one-shot create-thread state.

## Testing

- Unit tests cover `/projects`, project menu callbacks, `/newchat` and `/newthread` title-then-prompt collection, title write-through, cancellation, expiry, restart persistence, successful create/start, missing thread id, and recoverable `turn/start` failure.
- Regression tests cover stale Plan choice buttons so old pending input cannot appear under a newer `[commentary]`.
- Live Telegram E2E must use readback: project menu -> `New thread` -> prompt -> `[Final]`, plus a Plan Mode choice scenario.

## Acceptance Criteria

- [ ] `/projects` shows project workspace buttons grouped by normalized cwd.
- [ ] `New thread` creates a new thread in the selected project cwd and starts the first turn.
- [ ] The current chat/topic is bound to the new thread after success.
- [ ] Stale Plan choice buttons do not appear under a different turn's `[commentary]`.
