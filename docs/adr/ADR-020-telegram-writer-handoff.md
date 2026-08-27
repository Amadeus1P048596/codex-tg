# ADR-020: Telegram Writer Handoff

- Status: accepted
- Amends: ADR-012, ADR-019
- Related: `docs/process/telegram-writer-handoff-brief.md`

## Context

Codex thread history is durable and can be read by multiple clients, but an App
Server process holds the active writer for a loaded thread. The Telegram bridge
previously called `thread/resume` during bind, observer bootstrap, periodic
attachment, repair, and restart. A read-only Telegram route therefore prevented
Codex Desktop from loading the same thread and repair immediately recreated the
conflict.

App Server `thread/unsubscribe` removes a connection subscription but retains an
unsubscribed loaded thread for an inactivity grace period. It is not an immediate
writer-handoff primitive for the spawned stdio process.

## Decision

- Observer-only tracking and polling use thread list/read operations and never
  acquire writers.
- `/bind` and `Bind here` are explicit Telegram ownership actions. They call
  `thread/resume`, record the acquired writer, and background attachment restores
  ownership after repair or daemon restart.
- A manual-release marker is persisted per thread. Released bindings stay routed
  but background attachment and restart skip them until the operator binds again
  or sends new Telegram input.
- Telegram also calls `thread/resume` immediately before explicit input or turn
  control, clearing a manual-release marker after successful acquisition.
- A resume error indicating another active writer is a user-visible conflict.
  Telegram does not queue the message, steal ownership, or start a parallel turn.
- The daemon records thread ids successfully acquired by the current live App
  Server generation.
- `/release` and the `Release TG lock` session-card button validate every recorded thread through the authoritative live App
  Server. If any is active, waiting, or unverifiable, it fails closed. Otherwise
  it replaces only the live App Server session and clears the generation-local
  ownership set. The read-only poll session remains connected.
- Release is session-wide because the writer belongs to the spawned App Server
  process. The command does not promise per-thread unloading.

## Consequences

- Telegram and Codex Desktop share durable history and can alternate as writer,
  but they cannot write the same thread concurrently.
- Binding implies Telegram ownership; observing without binding does not.
- While Telegram owns a thread, summary and Final cards show `Release TG lock`
  instead of `Bind here`.
- After five minutes without an allowed Telegram message or button action, the
  daemon runs the same fail-closed, live-session-wide release used by
  `/release`. Active, pending, or unverifiable threads defer release; new
  Telegram activity cancels an in-flight idle decision and resets the timer.
- `/repair` reacquires bound writers unless they carry the persisted manual-release marker.

## Non-goals

- Simultaneous multi-writer support.
- Writer preemption.
- Public App Server transport changes.
- Automatic release during approval, user-input, or active-turn states.
