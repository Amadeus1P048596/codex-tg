# Telegram Writer Handoff Feature Brief

## Problem

The original shared-runtime implementation resumed a thread on the Telegram live
App Server even when Telegram was only reading. The current deployment instead
uses a dedicated `CTR_GO_CODEX_HOME`: Desktop and Telegram do not share sessions,
runtime databases, writer locks, caches, or App Server processes. The remaining
problem is to manage the live writer safely inside the Telegram runtime while a
second App Server connection performs read-only polling.

## Goal

Make explicit continuation load and retain the Telegram live writer, keep passive
observation read-only, reject unexpected same-runtime writer conflicts without
queuing a message, and let the operator recycle Telegram's idle live session safely.

## Non-goals

- Multiple simultaneous writers for one Codex thread.
- Sharing or taking a writer lock from Codex Desktop.
- Linking Desktop and Telegram session or runtime databases.
- Automatically recycling a live session while a Telegram turn is active.
- Changing durable Codex thread identity or history storage.

## UX / Operator Flow

- `/bind` and `在 TG 中继续` call `thread/resume` and retain ownership. Repair and
  daemon restart reacquire bound writers unless they were manually released.
- A plain message, `/reply`, or `/plan` calls `thread/resume` immediately before
  the write. If another connection in the isolated runtime owns the thread, Telegram reports the conflict
  and does not queue or create a parallel turn.
- While the live App Server owns a session, summary and Final cards show
  `释放空闲写入权` in place of `在 TG 中继续`.
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
- Unit: `在 TG 中继续` acquires ownership and changes the card action to release.
- Unit: writer-conflict resume returns a direct response and does not start or
  steer a turn.
- Unit: `/release` refuses active/unverifiable owned threads.
- Unit: `/release` replaces only the idle live session and leaves poll intact.
- Regression: daemon, Telegram command-menu, full Go test, build, and secret scan.
- Live: confirm Desktop and Telegram run different App Server processes and that
  recycling the TG live session leaves its read-only poll session connected.

## Acceptance Criteria

- [x] Binding acquires a writer; passive observation does not.
- [x] Explicit Telegram input acquires a writer only at dispatch time.
- [x] Another writer produces a clear no-queue/no-steal response.
- [x] `/release` never closes a live session with an active or unknown owned thread.
- [x] An idle release closes and recreates only the Telegram live session.
- [x] Manual release persists across background attachment and daemon restart.
