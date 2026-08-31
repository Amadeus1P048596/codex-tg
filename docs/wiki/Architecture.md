# Architecture

`codex-tg` is moving from a Telegram-first bridge toward a local Codex Control
Plane. The current production adapter is Telegram; future adapters can include
tray workflows, local HTTP/unix-socket clients, voice assistants, or a separate
router agent.

```text
Channel adapters
  - Telegram
  - macOS tray
  - future voice / router / local API
        |
        v
codex-control core
  - thread and turn lifecycle
  - event normalization
  - approvals and user input
  - notification policy
  - durable routing state
        |
        v
Codex connectors
  - App Server
  - optional SDK/MCP orchestration adapters
        |
        v
local Codex sessions and workspaces
```

## Current Runtime

The v0.5 runtime runs as a Go daemon with:

- Telegram Bot API long polling;
- route and callback handling;
- observer and panel rendering;
- SQLite state;
- local Codex App Server connectivity.

The current implementation starts two `codex app-server` children over stdio in
a dedicated `CTR_GO_CODEX_HOME`: one live session for writes and events, plus a
read-only poll session for authoritative reconciliation. These children do not
reuse the Windows Codex Desktop App Server or its mutable runtime files.

Desktop and Telegram keep separate sessions, state/history SQLite files, writer
locks, caches, and App Server lifecycles. Explicit links may share static
capabilities such as Skills, plugins, packages, and global instructions. Durable
cross-client memory uses a separate application-level store for user-approved
facts and preferences; it does not link either runtime's built-in memory DB.

Scheduled tasks follow the same runtime-isolation rule. Thread-scoped App Server
config injects a private stdio MCP tool into new and resumed Telegram threads;
the tool writes validated standalone cron definitions under the Telegram home.
The daemon evaluates local-time RRULEs, durably claims due slots in SQLite, and
starts each run as a new TG-private App Server thread. Observer/Home/inbox own
Telegram visibility. Desktop task definitions and scheduling are not read or
written. See ADR-023.

ADR-019 allows future work to prepare official App Server `unix://` and
`app-server proxy` transports when they improve lifecycle safety.

## Integration Surface

App Server is the authoritative control surface for interactive Codex state:
threads, turns, approvals, user input, live events, history, and snapshots.

SDK and MCP integrations may be used as orchestration adapters for router-agent
workflows, Agents SDK handoffs, traces, or multi-agent experiments. They do not
replace App Server state for live rendering, Details, approvals, or notification
truth in v0.5.

## Observer Model

The live App Server provides notifications for Telegram-started runs. The
isolated poll App Server covers background or externally initiated work that is
still inside the Telegram runtime through bounded `thread/read` polling. Windows
Codex Desktop runs are outside this runtime and are not observed. Local session
JSONL is reserved for explicit log exports, not live observer state.

## State

SQLite stores routes, callback tokens, bindings, observer target, panels,
pending prompts, delivery metadata, and daemon state.

Future control-plane work should keep adapter-specific state, such as Telegram
message ids, outside the control core wherever practical.

## Further Reading

- [Control Plane](Control-Plane.md)
- [ADR-019: Codex Control Plane](../adr/ADR-019-codex-control-plane.md)
- [ADR-023: Telegram-Native Scheduled Tasks](../adr/ADR-023-telegram-native-scheduled-tasks.md)
