# Unified Codex Settings Brief

## Problem

Telegram-started conversation turns read the SQLite-backed `/model` and
`/effort` settings, but scheduled tasks currently carry and execute independent
model values. Existing thread metadata can also become an implicit model
fallback when the shared setting is `Auto`. Speed also needs one runtime-wide
authority instead of inheriting an old thread tier. The result otherwise is
more than one execution-settings authority inside the same Telegram runtime.

## Goal

Use the SQLite-backed Codex model, reasoning-effort, and speed settings as the
single execution preference for every future Telegram turn: existing
conversations, new conversations, and codex-tg scheduled runs.

## Non-goals

- Changing or interrupting a turn that is already running.
- Sharing model settings with Codex Desktop.
- Maintaining a static model catalog outside App Server `model/list`.
- Rewriting App Server thread history or metadata.

## Contract

- `codex.model`, `codex.reasoning_effort`, and `codex.speed` are the shared
  execution settings.
- Existing and new conversation turns read them at `turn/start` time.
- Scheduled runs read the same settings at `turn/start` time and do not use
  task-local model fields.
- `Auto` sends no model or reasoning override, including for an existing thread;
  App Server chooses its current defaults.
- Legacy task files may retain model fields for file compatibility, but those
  fields are not execution authority and new task creation no longer requires
  them.
- `/model` remains populated and validated from the current private App Server.
- `/speed` offers `normal` and `fast`. The bridge maps these to App Server
  `turn/start.serviceTierForTurn` values `default` and `fast` so the selection
  applies to the new turn without changing one already in progress.

## Acceptance Criteria

- A scheduled task whose legacy model differs from the shared setting starts
  with the shared model and reasoning effort.
- Existing-thread and new-thread option resolution produce identical model and
  reasoning and speed values.
- A scheduled run receives the same speed selection as an interactive turn.
- New scheduled tasks can be created without task-local model settings.
- Settings copy and public docs describe all future Telegram and scheduled
  turns, rather than Plan Mode alone.
