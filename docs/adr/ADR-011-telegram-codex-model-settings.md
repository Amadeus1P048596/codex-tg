# ADR-011: Telegram Codex Execution Settings

- Status: accepted
- Decision: model, reasoning-effort, and speed selection for all future Telegram turns and scheduled runs is a Telegram UI feature backed by SQLite state.

## Context

Telegram starts ordinary, Plan Mode, new-conversation, and scheduled turns in its isolated App Server runtime. Their model settings should not be hardcoded in environment variables or copied into every thread and task because operators need one visible control on the same Telegram surface they use to start runs.

The settings are runtime-wide operator preferences for future `turn/start` calls, not per-message content and not observer-rendered history. They do not interrupt a turn that is already running.

## Decisions

- `/settings` shows the shared Codex model, reasoning effort, and speed used for future Telegram conversation turns and scheduled runs.
- `/model` opens a button menu populated from App Server `model/list`.
- `/effort` opens a button menu for reasoning effort. When a selected model advertises supported reasoning efforts, the menu is narrowed to that model.
- `/m` is the compact read-only command for the current model and reasoning effort.
- `/sm` is the compact two-step editor: it first presents the App Server model list, then presents only the reasoning efforts supported by the selected model.
- `/speed` opens a two-choice menu for `normal` and `fast` and shows the currently selected value.
- Selecting a model or reasoning effort persists the value in SQLite daemon state.
- Selecting a speed persists `codex.speed` in SQLite daemon state. Missing or invalid legacy state resolves to `normal`.
- Each future `turn/start` maps `normal` to `serviceTierForTurn: "default"` and `fast` to `serviceTierForTurn: "fast"`. A speed change therefore affects existing, new, and scheduled conversations on their next turn without rewriting the thread's stored tier.
- Selecting `Auto` clears the explicit value and lets App Server defaults or discovered defaults apply.
- Existing conversations, new conversations, and codex-tg scheduled runs read the same SQLite values when starting their next turn. Existing thread metadata is not an execution fallback.
- Scheduled task definitions do not own model or reasoning-effort settings. Legacy fields may remain in old task files for file compatibility, but the scheduler ignores them.
- During `/sm`, the model selection edits the same message into the reasoning-effort step. After the final selection, the message is edited into a compact settings summary without inline choice buttons. The operator can reopen the compact flow with `/sm` or use `/settings`, `/model`, and `/effort` compatibility controls.
- These settings are not public env configuration and must not introduce local-only secrets or private identifiers into docs or commits.

## Consequences

- Settings survive daemon restarts because SQLite remains the local source of truth.
- Changing one setting affects the next turn across all Telegram execution paths without rewriting every thread or task.
- `fast` can consume more Codex credits; the Telegram menu states this tradeoff rather than presenting it as a free performance toggle.
- Telegram UX avoids stale/replayable choice menus after a selection.
- App Server remains the source of model availability; the bridge does not maintain a static model catalog.
- Unit tests must cover read-only status, sequential menu rendering, model-specific effort filtering, speed-to-service-tier mapping, persistence, scheduled-run propagation, and button removal after selection.
- Live Telegram validation should verify rendered menus and callback behavior when a live contour is available.
