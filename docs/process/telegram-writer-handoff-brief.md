# Telegram Writer Handoff Feature Brief

## Problem

Binding or observing a Codex thread currently resumes it on the Telegram live
App Server. That acquires the thread writer lock even when Telegram is only
reading, so Codex Desktop cannot load the same durable thread. Repair and daemon
restart repeat the resume and recreate the conflict.

## Goal

Make explicit binding acquire and retain Telegram ownership, keep passive
observation read-only, reject another-client writer conflicts without queuing a
message, and let the operator release Telegram's idle writer session safely.

## Non-goals

- Multiple simultaneous writers for one Codex thread.
- Taking a writer lock away from Codex Desktop or another App Server.
- Automatically recycling a live session while a Telegram turn is active.
- Changing durable Codex thread identity or history storage.

## UX / Operator Flow

- `/bind` and `Bind here` call `thread/resume` and retain ownership. Repair and
  daemon restart reacquire bound writers unless they were manually released.
- A plain message, `/reply`, or `/plan` calls `thread/resume` immediately before
  the write. If another client owns the thread, Telegram reports the conflict
  and does not queue or create a parallel turn.
- While TG owns a session, summary and Final cards show `Release TG lock` in
  place of `Bind here`.
- `/release` and the release button check every thread written through the current Telegram live
  session. If any is active or cannot be verified, release is refused. Otherwise
  only the Telegram live App Server session is recycled, releasing all of its
  idle writer locks while the read-only polling session continues.
- Five minutes without an allowed Telegram message or button action triggers the
  same guarded release. New Telegram activity resets the timer, and active,
  pending, or unverifiable threads are retried later rather than forced closed.

## Domain Model

- `liveOwnedThreads`: in-memory thread ids resumed successfully by the current
  Telegram live-session generation.
- `writer.telegram.released.<thread_id>`: persisted opt-out from automatic
  bound-thread reacquisition after manual release.
- A release is live-session-wide because writer locks belong to that App Server
  process, not to Telegram bindings.

## Architecture

The daemon owns writer acquisition and release. Telegram command handling calls
one daemon release operation; binding and observer code consume only list/read
operations. App Server remains authoritative for active-turn state.

## Testing

- Unit: bootstrap resumes bound threads but skips manually released bindings.
- Unit: `Bind here` acquires ownership and changes the card action to release.
- Unit: writer-conflict resume returns a direct response and does not start or
  steer a turn.
- Unit: `/release` refuses active/unverifiable owned threads.
- Unit: `/release` replaces only the idle live session and leaves poll intact.
- Regression: daemon, Telegram command-menu, full Go test, build, and secret scan.
- Live: restart the installed daemon and confirm previously bound threads are no
  longer locked by the Telegram bridge.

## Acceptance Criteria

- [x] Binding acquires a writer; passive observation does not.
- [x] Explicit Telegram input acquires a writer only at dispatch time.
- [x] Another writer produces a clear no-queue/no-steal response.
- [x] `/release` never closes a live session with an active or unknown owned thread.
- [x] An idle release closes and recreates only the Telegram live session.
- [x] Manual release persists across background attachment and daemon restart.
