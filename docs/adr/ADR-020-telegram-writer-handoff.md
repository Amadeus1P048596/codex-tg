# ADR-020: Telegram Writer Handoff

- Status: accepted
- Amends: ADR-012, ADR-019
- Related: `docs/process/telegram-writer-handoff-brief.md`

## Context

Codex thread history is durable, but an App Server process holds the active
writer for a loaded thread. Before the private-runtime boundary in ADR-019 was
adopted, the Telegram bridge and Codex Desktop could point at the same mutable
runtime. Telegram then called `thread/resume` during bind, observer bootstrap,
periodic attachment, repair, and restart, so a read-only route could acquire the
writer and recreate cross-client conflicts.

The current deployment does not share that runtime. Telegram uses a dedicated
`CTR_GO_CODEX_HOME` and owns separate live and poll App Server children. This ADR
now governs writer lifecycle inside that isolated Telegram runtime; it is not a
Desktop-to-Telegram session handoff contract.

App Server `thread/unsubscribe` removes a connection subscription but retains an
unsubscribed loaded thread for an inactivity grace period. It is not an immediate
writer-handoff primitive for the spawned stdio process.

## Decision

- Observer-only tracking and polling use thread list/read operations and never
  acquire writers.
- `/bind` and `在 TG 中继续` are explicit live-session actions. They call
  `thread/resume`, record the acquired writer, and background attachment restores
  ownership after repair or daemon restart.
- A manual-release marker is persisted per thread. Released bindings stay routed
  but background attachment and restart skip them until the operator binds again
  or sends new Telegram input.
- Telegram also calls `thread/resume` immediately before explicit input or turn
  control, clearing a manual-release marker after successful acquisition.
- A resume error indicating another active writer in the isolated runtime is a
  user-visible conflict. Telegram does not queue the message, steal ownership,
  or start a parallel turn.
- The daemon records thread ids successfully acquired by the current live App
  Server generation.
- `/release` and the `释放空闲写入权` session-card button validate every recorded thread through the authoritative live App
  Server. If any is active, waiting, or unverifiable, it fails closed. Otherwise
  it replaces only the live App Server session and clears the generation-local
  ownership set. The read-only poll session remains connected.
- Release is session-wide because the writer belongs to the spawned App Server
  process. The command does not promise per-thread unloading.
- On affected Windows App Server versions, `thread/resume` rewrites the stored
  rollout to an extended drive path and caches it, while `thread/archive` fails
  on that loaded path. The client tracks resumed thread ids per live generation.
  Archive recycles that generation only after every owned writer is verified
  idle, delegates archive to the fresh App Server before any resume, and then
  restores the non-target writers. An active or unverifiable writer fails closed.

## Consequences

- Telegram and Codex Desktop keep separate thread histories, runtime databases,
  writer locks, caches, and App Server lifecycles.
- Selected capabilities may be linked between homes, and user-approved durable
  memory may use a separate application-level store. Neither mechanism shares
  conversation or writer state.
- Binding implies Telegram ownership; observing without binding does not.
- While the live App Server has loaded a thread, summary and Final cards show
  `释放空闲写入权` instead of `在 TG 中继续`.
- After five minutes without an allowed Telegram message or button action, the
  daemon runs the same fail-closed, live-session-wide release used by
  `/release`. Active, pending, or unverifiable threads defer release; new
  Telegram activity cancels an in-flight idle decision and resets the timer.
- `/repair` reacquires bound writers unless they carry the persisted manual-release marker.

## Non-goals

- Simultaneous multi-writer support.
- Writer preemption.
- Sharing Desktop session databases or conversation history with Telegram.
- Public App Server transport changes.
- Automatic release during approval, user-input, or active-turn states.
