# ADR-022: Telegram Scheduled Tasks Bridge

- Status: superseded by ADR-023
- Amends: ADR-019
- Related: `docs/process/telegram-scheduled-tasks-brief.md`

ADR-022 records the original Desktop-store bridge. Runtime investigation showed
that writing a native-looking file does not perform the Desktop host's private
task-registration lifecycle and cannot route a run back into Telegram. ADR-023
therefore replaces the shared-store design with a Telegram-owned scheduler.

## Context

Codex Desktop provides Scheduled tasks through a host-injected
`automation_update` tool. That tool is not part of a standalone App Server, so
Telegram threads in the private `CTR_GO_CODEX_HOME` cannot call it. Linking
Desktop and Telegram runtime databases would violate ADR-019 and reintroduce
writer conflicts.

Scheduled heartbeat tasks also target a specific local thread. A Telegram
thread id has no meaning in the Desktop runtime, even when both clients use the
same workspace.

## Decision

- `CTR_GO_AUTOMATIONS_DIR` is an explicit, optional path to the Codex Desktop
  native automation-definition directory.
- codex-tg injects a private stdio MCP server into both `thread/start` and
  `thread/resume` through thread-scoped App Server config.
- The MCP server exposes a narrow `automation_update` interface and writes only
  validated native `automation.toml` task definitions under that directory.
- Telegram-created tasks are standalone `cron` tasks with local execution.
  Heartbeat tasks are rejected because they cannot safely target an isolated
  Telegram thread.
- Codex Desktop remains the single scheduler and owns execution, run history,
  UI, and notifications. codex-tg never runs a parallel schedule loop.
- Existing installations remain disabled until the path is configured. New
  init/service-install flows propose the conventional Desktop path.
- Automation prompts are local plaintext and must not contain credentials.

## Consequences

- Desktop and Telegram sessions, App Servers, runtime SQLite, locks, caches,
  thread identity, and conversation history remain isolated.
- Scheduled task definitions become a deliberate shared mutable boundary, but
  the writer is restricted to one validated file shape and safe child paths.
- Existing and newly created Telegram threads gain the tool after resume/start;
  rollout history does not need to be rewritten.
- Local Scheduled tasks still require Codex Desktop to be running.
- Scheduled results remain a Desktop surface and are not represented as turns
  in the Telegram runtime.

## Non-goals

- A Telegram-native scheduler.
- Cross-runtime heartbeat continuation.
- Scheduled-run observation or notification forwarding to Telegram.
- Public MCP or App Server network exposure.
