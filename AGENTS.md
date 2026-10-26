# AGENTS

Purpose: help AI agents work on `codex-tg` without increasing complexity or weakening the operator-facing Telegram control loop.

Good code is code that is easy to understand, change, test, and safely extend. Good agent work is evidence-backed, small in scope, and validated through the same surfaces the operator uses.

## Repository Purpose

`codex-tg` is a Go daemon evolving into a local Codex Control Plane. Telegram is the first production adapter for observing, steering, approving, and routing local Codex App Server work.

The repo is public-facing. Keep every change safe for open-source publication: no private paths, tokens, Telegram ids, local sessions, databases, logs, screenshots with private data, or environment-specific credentials.

## Working Mode

- First understand the current design. Use the Context Pull Map and inspect only the nearby code, tests, ADRs, and contracts relevant to the requested change before editing.
- Resolve routine ambiguity from existing context and safe read-only inspection. Ask only when a missing choice would materially change the result, require new authority, or make an action difficult to reverse; otherwise make a reasonable assumption, continue, and disclose it.
- Prefer small steps: inspect -> plan -> test -> implement -> refactor -> validate.
- Do not generate large rewrites, broad refactors, or speculative frameworks unless the task explicitly requires them.
- Build what is needed now. Avoid adding libraries, build tools, abstractions, or cross-platform machinery without an immediate reason.
- Match the current user's language for working plans, handoffs, and status updates when clear. Public README, wiki, demo, release notes, and GitHub-facing docs may stay English-first.
- Lead with the outcome and communicate in concise, plain paragraphs. Use lists or headings only when they make the result easier to scan; avoid ornamental formatting and repetition.

## Agent Workflow Kernel

- Meaningful or cross-cutting feature work needs a destination before implementation: a short issue, feature brief, or equivalent written target. For a small fix, the user request plus a focused regression test may be the target.
- Slice vertically. Prefer one small end-to-end behavior over horizontal phases such as storage first, daemon later, UI last.
- Use TDD for non-trivial behavior changes and bug fixes: failing or updated test, minimal implementation, refactor, targeted checks.
- Use an independent fresh-context reviewer for meaningful changes when the current environment and user instructions permit it. Otherwise perform a structured self-review of the issue or brief, diff, tests, ADRs/rules, and validation output, and state that the review was not independent.
- Do not spawn subagents unless the current environment and user instructions permit it. When permitted, delegate only concrete, bounded, independent work that materially improves latency or quality.
- A task hosted by codex-tg must not unload, kill or restart its own daemon/App Server, including via raw launchctl or a migration script. Finish the task and use an independent maintenance process; do not bypass a hosted-task lifecycle guard. Already-applied migrations must be idempotent.
- Prepare the candidate and validation plan without mutating the live service. Restart the live daemon or send real Telegram QA messages only with explicit current operator authorization.
- After compaction, context reset, or handoff, reread this file first, then pull only the docs called out by the Context Pull Map.

## Context Pull Map

- Telegram UI, routing, observer panels, lifecycle, Plan Mode, diagnostics, or rendering: read `docs/testing/regression-map.md` and the ADR named there when present.
- Public command behavior or Telegram product contract: read `docs/research/contract-matrix.md` when present.
- New feature planning, bugfix shaping, or review handoff: read `docs/process/agent-workflow.md` and the related template when present.
- Release, README, demo, or public positioning work: read README plus relevant `docs/wiki/*`, `docs/demo/*`, `CONTRIBUTING.md`, and `SECURITY.md` when present.
- If a referenced document is absent in the current branch, do not invent it or add broken links. Use this `AGENTS.md`, nearby code/tests, and existing ADRs as the source of truth.

## Core Decisions

- Current runtime state authority is Codex App Server. New control-plane work must preserve App Server authority for interactive threads, turns, approvals, live events, history, and snapshots.
- Spawned `codex app-server` over stdio remains supported, but ADR-019 allows future `unix://` and `app-server proxy` transport work.
- Durable identity is `threadId`; Telegram chat/topic is only an input and rendering surface.
- Live observer events come from the daemon live session; non-Telegram activity
  inside the isolated Telegram runtime must also be covered by polling
  `thread/read`. Windows Codex Desktop uses a separate runtime and is not an
  observer source.
- Desktop and Telegram must not share App Server processes, session/history
  databases, writer locks, or caches. Explicitly linked capabilities and a
  separate user-approved durable-memory store are allowed; conversation and
  runtime state are not.
- App Server session lifecycle transitions must be serialized and generation-aware; stale old-session close/error events must not invalidate newer sessions or create repair loops.
- Startup must remain non-blocking; never put full thread sync into synchronous startup.
- SQLite is the local source of truth for bindings, routes, callbacks, panels, observer target, delivery metadata, and daemon state.
- Model availability is authoritative only when returned by the configured Telegram-private App Server `model/list`. After a runtime or model change, verify the live and poll executable versions, the model list, and the actual `turn/start` model/effort in logs or readback; configuration files alone are not sufficient evidence.
- Do not add a second live runtime backbone for App Server state. SDK/MCP may be used only where ADR-019 allows orchestration adapters, and must not replace App Server truth for live UI/control state without a new ADR.

## Design And Code Principles

- Simple code beats clever code. Keep changes local and avoid making unrelated subsystems harder to understand.
- Prefer cohesive vertical slices and deep modules: expose small, clear interfaces that hide useful internal complexity.
- Keep modules focused. Each module should have one main reason to change.
- Make interfaces explicit with typed inputs/outputs, clear ownership, and precise error cases.
- Avoid duplication. If the same routing, rendering, lifecycle, or parsing rule appears twice, extract one source of truth.
- Inject or pass dependencies for IO, time, config, external services, and randomness. Do not hardcode them deep in business logic.
- Make invalid states hard to represent when practical, especially around thread/turn/panel lifecycle and callback routing.
- Use domain terms consistently: thread, turn, panel, observer target, route, callback, Plan prompt, Final Card, Details.
- Do not rename domain concepts casually. A renamed concept must improve clarity across code, tests, ADRs, and docs.
- Prefer guard clauses and early returns over deeply nested conditionals.
- Follow existing project conventions and standard formatters. Do not invent a new style.
- Comments should explain intent, constraints, tradeoffs, or non-obvious decisions. Do not comment obvious syntax.

## Telegram UX Invariants

- Global observer is a single target. `/observe all` moves it; `/observe off` disables passive monitoring.
- One Codex turn normally owns one Telegram activity card. Use `typing` for the
  first four seconds, then create Working only if the turn is still active.
- Aggregate raw commentary, tool, output, and lifecycle events. Do not render
  `[Tool]`, `[Output]`, empty-output placeholders, or one Telegram message per event.
- Active card edits have a four-second minimum interval. Needs-input and terminal
  transitions bypass the throttle.
- Finalization sends one normally notifying terminal card, persists it as the
  panel summary, then deletes the old Working card best-effort. A fast turn
  sends the same terminal card without a replacement step; a long final may
  continue in silent `Codex · Result` companion messages.
- The leading emoji is conversation identity, not state. The primary Telegram
  UI uses the Chinese state labels `处理中`, `需要输入`, `已完成`, `失败`, and
  `已取消`; internal protocol state names remain unchanged.
- `Details` pagination edits the same message; it must not create a stream of replacement pages.
- `Details` is the primary on-demand inspection surface. `Tools file` is available from Details when needed; `get_full_log` exists only for compatibility with legacy cards. Automatic per-tool document spam is forbidden.
- Document exports should use in-memory multipart upload. Temporary Telegram
  photo inputs must be private, bounded, and removed after App Server accepts them.
- Plan Mode waiting input uses the routeable activity card. Buttons are allowed
  only for structured choices provided by Codex.
- Telegram-originated Plan Mode starts must pass App Server `collaborationMode.mode = plan`; prompt wording alone is not Plan Mode.
- `/model`, `/effort`, and `/speed` are Telegram button menus for shared execution settings used by every future Telegram turn and scheduled run. Persist selections in SQLite, not env-only local config. Map `normal`/`fast` speed to App Server's per-turn `default`/`fast` service tier. After a selection, remove the inline choice buttons from the edited message.
- Replies to active turns should steer the active turn. If steering is rejected while the thread still looks active, do not start a parallel turn.
- Stale active-turn ghosts are different: if `thread/read` exposes a final answer or `turn/steer` says `no active turn to steer`, follow the branch's lifecycle ADR or contract note when present and allow a new `turn/start` after re-read instead of returning a false parallel-turn warning.
- Every activity card includes the emoji marker, Codex label, textual state, and
  short `T:`/`R:` metadata when available. Full ids are opt-in diagnostics.
- Emoji markers are visual hints only. Persisted message routes and callback tokens are the routing authority.

## Routing Precedence

1. Explicit thread id in a command.
2. Reply-to routed message.
3. Armed one-shot steer/answer state.
4. Bound thread for the current chat/topic.

## Collaboration Contract

- Screenshots, Telegram message ids, daemon logs, CI logs, and user-observed symptoms are primary evidence. Investigate them before generalizing.
- If private env/auth/chat/thread/session state is required to complete the current task and is unavailable, ask for it; otherwise continue with safe public or read-only evidence. Never invent credentials, route ids, Telegram chat ids, thread ids, or live state.
- If a Skill or repository rule causes a pause, permission request, unfinished result, or divergence from the user's requested outcome, name the exact file and rule and distinguish a mandatory requirement from the agent's interpretation.
- Keep an explicit original-blockers checklist in long tasks and state which initial problems are fixed, partially fixed, or still open.
- If the same symptom repeats, stop patching the surface and inspect the lower layer: app-server session lifecycle, poll/live race, terminal gating, routing state, or App Server state.
- Separate readback identity, Bot API delivery/routing identity, and durable Codex identity. Do not conflate Telegram readback chat, Bot API chat id, and `threadId`.

## Definition Of Done For Telegram-Facing Changes

- For UI, routing, observer, lifecycle, Plan Mode, Details, Markdown rendering, or callback changes, unit tests are not enough when a live contour is available and the operator has authorized live mutation.
- Prepare the rebuild and candidate first. After explicit operator authorization, restart the daemon if needed, perform real Telegram readback, inspect edited messages, exercise callbacks when relevant, and correlate daemon logs or SQLite when lifecycle is involved.
- Bot API send/edit success alone is not sufficient.
- If live E2E is unavailable or not authorized, state the exact constraint and mark the change as not fully validated.
- During iteration run the smallest relevant check first. Use broader checks according to Testing And TDD Discipline and Required Checks.

## Testing And TDD Discipline

- Use tests as the main feedback loop for non-trivial behavior. Add or update tests for behavior changes and bug fixes; reversible, low-impact documentation or configuration edits need proportionate checks rather than artificial mirror tests.
- Prefer TDD for non-trivial changes: write or update a failing test, make it pass, then refactor.
- Test public behavior through stable interfaces. Mock only external boundaries or slow/non-deterministic dependencies.
- Keep tests deterministic, isolated, and runnable without manual setup.
- Run the smallest relevant tests while iterating. Before commit or release, run the Required Checks once after the relevant changes settle. Do not repeat an unchanged passing suite unless related code changed or a new failure makes it relevant.
- If a check cannot be run, say exactly why and what should be run manually.
- For feature/test context before changing Telegram routing, observer panels, Plan Mode, diagnostics, Telegram rendering, or lifecycle recovery, read `docs/testing/regression-map.md` and the ADR named there when present.
- Behavior changes need the matching ADR or contract note, regression-map/test anchors when present, and validation notes when live Telegram was used.

## Branch, Commit, And Release Discipline

- Keep diffs focused on the requested task. Do not mix broad refactors with feature or bugfix work.
- When multiple branches are active, maintain a branch ledger: branch, purpose, base, latest commit, remote status, and done/not done.
- Before committing, check dirty tree, staged diff, `git config user.name`, `git config user.email`, tests, and secret scan.
- Before pushing, verify the current branch, upstream, ahead/behind state, and that the intended commits are included.
- Release or branch handoff summaries must include branch, commit hash, push status, checks run, docs/ADR changed, and daemon version/restart status when applicable.

## Repository Layout

- `cmd/ctr-go/` daemon CLI entrypoint.
- `internal/appserver/` JSON-RPC stdio client and snapshot normalization.
- `internal/config/` env-driven config.
- `internal/daemon/` runtime orchestration, observer, panels, callbacks, log exports.
- `internal/model/` shared types.
- `internal/storage/` SQLite schema and repositories.
- `internal/telegram/` Telegram Bot API transport.
- `internal/tgformat/` Markdown-to-Telegram entity renderer.
- `tests/` black-box and live-gated tests.
- `docs/` ADRs, wiki pages, demo docs, metrics, acceptance notes.

## Private Vs Public Context

- Keep private deep handoff/debug files outside the repo.
- Do not commit raw Telegram ids, local sessions, raw logs, private screenshots, local E2E runners, tokens, phone numbers, local paths, `.env`, databases, or runtime artifacts.
- Do not commit giant generated docs, logs, or plans.
- Public docs should describe durable behavior and contracts, not private investigation history.

## Change Rules

- Remove dead code introduced by the change.
- Update README and public docs when public behavior changes.
- Update ADRs when behavior or architecture changes. Supersede old ADRs instead of silently contradicting them.
- Do not hide uncertainty. Call out assumptions, remaining risks, and checks that could not be run.
- Final summaries should state what changed, what was tested, and what risk remains.

## Required Checks

Run before committing code changes:

```powershell
go test ./...
go build -buildvcs=false ./...
```

Run a targeted secret/local scan before committing or publishing:

```powershell
rg -n "BOT_TOKEN|TELEGRAM_BOT_TOKEN|api_hash|api_id|phone|password|secret|\\.session|\\.sqlite|\\.env|C:\\\\Users\\\\<private-user>" .
```

For docs-only changes, at minimum run `git diff --check`, a targeted secret/local scan on edited docs, and `git status --short`.

## Demo Rules

- Demo tests must be gated by explicit env vars and build tags.
- Screenshot demo content must be in English and safe for public display.
- Demo screenshots must be reviewed manually before committing.
- Do not fake product screenshots. Use the real Telegram renderer and Bot API.
