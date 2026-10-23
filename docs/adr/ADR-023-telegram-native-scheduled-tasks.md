# ADR-023: Telegram-Native Scheduled Tasks

- Status: accepted
- Supersedes: ADR-022
- Amends: ADR-019
- Related: `docs/process/telegram-scheduled-tasks-brief.md`
- Related: `docs/process/codex-tg-push-service-brief.md`

## Context

The first Scheduled tasks bridge wrote validated `automation.toml` files into
the Codex Desktop task directory and expected Desktop to execute them. A task
file is only one part of Desktop's host-owned automation lifecycle: an external
write does not perform the private registration/reload operation. A due task can
therefore be present on disk without running. Even if Desktop runs a task, its
history and notification belong to the Desktop Scheduled surface, so codex-tg
has no authoritative Telegram thread or turn to observe.

Sharing the task directory also made Desktop availability and reload behavior a
hidden dependency of the Telegram feature. This conflicts with the existing
rule that Desktop and Telegram own separate mutable runtime state.

## Decision

- `CTR_GO_AUTOMATIONS_DIR` points to a codex-tg-private task directory, defaulting
  to `~/.codex-tg/automations`. It must not point to Desktop's
  `~/.codex/automations` directory.
- The existing private stdio `automation_update` MCP tool continues to provide
  validated list/view/create/update/delete operations. New definitions carry an
  `owner = "codex-tg"` marker and may specify a working directory.
- The daemon evaluates supported local-time `HOURLY`, `DAILY`, `WEEKLY`, and
  `MONTHLY` RRULEs every 15 seconds. Monthly rules accept positive
  `BYMONTHDAY` values from 1 through 31 and skip months where a selected day
  does not exist.
- A due occurrence is claimed durably in daemon SQLite before App Server work is
  started. The same task/time-slot is therefore at-most-once across daemon
  restarts.
- If the Telegram App Server or observer target is temporarily unavailable, the
  occurrence remains unclaimed and is retried after readiness returns.
- Each claimed occurrence starts a new thread and turn in the Telegram-private
  App Server using the task prompt, optional cwd, and the same SQLite-backed
  model/reasoning settings as conversation turns. Task-local model fields are
  not execution authority. It never resumes a prior thread.
- Scheduled threads stay out of the current chat binding and the normal
  observer, Home, inbox, and activity-card surfaces. They are private execution
  contexts rather than user conversations.
- A successful `automation_update` completion records the creating Telegram
  thread id and user-facing title on the task without changing its scheduling
  `updated_at` boundary.
- Terminal or input-required scheduled states publish one standalone message
  through the daemon's unified push service. That message includes the task
  name and owning conversation title; `failed_runs_only` still suppresses a
  successful result. A successful run whose trimmed final response is exactly
  `DONT_NOTIFY` is recorded as complete without publishing a message.
- External private-network producers may use an optional authenticated
  `POST /v1/push` adapter. It is disabled by default, accepts no Telegram
  credentials or destination ids, and routes through the configured observer
  target with durable idempotency keys.
- A push idempotency key is claimed durably before Telegram delivery starts.
  An ambiguous timeout is not replayed, favoring at-most-once delivery over a
  duplicate operator notification.
- `heartbeat` remains unsupported because scheduled continuation would carry
  mutable conversation state and writer ownership across occurrences.

## Consequences

### Scheduled terminal notification safety

Scheduler-owned current turns share the transient `interrupted` grace gate
from ADR-012. An ambiguous poll snapshot must not update the run to cancelled
or publish a notification while that gate is deferring it. Explicit interrupts
and grace expiry remain terminal; recovered successful `DONT_NOTIFY` results
remain silent. This protection does not mark hidden runs as interactive chats.

Push identity uses the task, due slot, thread, turn, and notification phase
(needs-input or terminal), never a changing progress/final content fingerprint.
Late snapshots belonging to a previous occurrence cannot publish under the
current occurrence's slot or overwrite its run state.

- Telegram scheduling no longer depends on Codex Desktop being open, noticing a
  file change, or delivering a Desktop notification.
- Desktop and Telegram task definitions, schedulers, sessions, histories,
  writer locks, and caches are isolated. Skills and approved durable memory may
  still be shared through their separate explicit mechanisms.
- A task edit after a wall-clock slot takes effect from the next matching slot;
  it does not backfill the slot that preceded the edit.
- The daemon owns lightweight scheduling state, while App Server remains the
  authority for each scheduled thread and turn once execution begins.
- Scheduled execution remains isolated while notification routing, formatting,
  authentication, and de-duplication are shared with non-Codex producers such
  as the bank-gold monitor. External producers no longer need the Telegram Bot
  Token or chat id.
- Existing installations that explicitly pointed at Desktop's task directory
  must move Telegram-owned cron definitions into the private directory and
  update the config before deploying this version.

## Non-goals

- Importing or executing Desktop-owned heartbeat tasks.
- Mirroring Desktop Scheduled history.
- Public scheduling, a public push endpoint, or an App Server network listener.
- A general distributed scheduler or exactly-once side-effect guarantee inside
  a model turn; the guarantee covers only starting the same local time slot more
  than once.
