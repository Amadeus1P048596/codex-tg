# ADR-023: Telegram-Native Scheduled Tasks

- Status: accepted
- Supersedes: ADR-022
- Amends: ADR-019
- Related: `docs/process/telegram-scheduled-tasks-brief.md`

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
- The daemon evaluates supported local-time `HOURLY`, `DAILY`, and `WEEKLY`
  RRULEs every 15 seconds.
- A due occurrence is claimed durably in daemon SQLite before App Server work is
  started. The same task/time-slot is therefore at-most-once across daemon
  restarts.
- If the Telegram App Server or observer target is temporarily unavailable, the
  occurrence remains unclaimed and is retried after readiness returns.
- Each claimed occurrence starts a new thread and turn in the Telegram-private
  App Server using the task prompt, model, reasoning effort, and optional cwd.
  It never resumes a prior thread.
- Scheduled threads stay out of the current chat binding. Existing observer,
  Home, inbox, approval/input, and terminal-notification paths own their
  Telegram visibility.
- `heartbeat` remains unsupported because scheduled continuation would carry
  mutable conversation state and writer ownership across occurrences.

## Consequences

- Telegram scheduling no longer depends on Codex Desktop being open, noticing a
  file change, or delivering a Desktop notification.
- Desktop and Telegram task definitions, schedulers, sessions, histories,
  writer locks, and caches are isolated. Skills and approved durable memory may
  still be shared through their separate explicit mechanisms.
- A task edit after a wall-clock slot takes effect from the next matching slot;
  it does not backfill the slot that preceded the edit.
- The daemon owns lightweight scheduling state, while App Server remains the
  authority for each scheduled thread and turn once execution begins.
- Existing installations that explicitly pointed at Desktop's task directory
  must move Telegram-owned cron definitions into the private directory and
  update the config before deploying this version.

## Non-goals

- Importing or executing Desktop-owned heartbeat tasks.
- Mirroring Desktop Scheduled history.
- Public scheduling or App Server network listeners.
- A general distributed scheduler or exactly-once side-effect guarantee inside
  a model turn; the guarantee covers only starting the same local time slot more
  than once.
