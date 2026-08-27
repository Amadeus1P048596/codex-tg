# Validation Notes

- 2026-08-27 CST runtime-boundary review confirmed that Windows Codex Desktop
  and Telegram run separate App Server processes and private mutable
  `CODEX_HOME` state, while selected capabilities and an application-level
  user-approved memory store remain shareable. README, current architecture and
  writer-lifecycle docs, Telegram action copy, and new-install defaults were
  aligned with that model. Targeted CLI/Telegram/daemon tests, full
  `go test ./...`, `go build -buildvcs=false ./...`, `git diff --check`, and
  secret/local scans passed. The installed daemon was not rebuilt or restarted,
  so real Telegram readback of the renamed buttons remains pending.

- 2026-08-26 CST interactive session creation now collects an explicit title
  before a distinct first prompt for `/newchat` and `/newthread`, persists both
  stages across restart, derives interactive Chat folders from the title, and
  writes the title through to App Server. Short foreground completions retain
  the stable in-place card and add one de-duplicated audible terminal notice;
  fast terminal cards and separately sent long results remain single-notice
  paths. Targeted tests, full `go test ./...`, `go build -buildvcs=false ./...`,
  `git diff --check`, Python live-harness syntax validation, and the targeted
  secret/local scan passed. The Windows candidate and rollback copy are staged;
  the cutover is waiting for three consecutive idle checks, so live Telegram
  title/prompt and completion-notice readback is still pending.

- 2026-08-26 CST Telegram session-hub validation added `/home`, a persistent
  `/inbox`, foreground activation for newly created sessions, post-archive and
  post-restore next actions, user-owned title locking, active-session archive
  protection, and consistent Chinese copy on primary cards and buttons. Targeted
  navigation/admin tests plus full `go test ./...`, `go build -buildvcs=false
  ./...`, `git diff --check`, and the targeted secret/local scan passed. The
  rollback-capable Windows cutover waited for three consecutive idle checks,
  preserved the prior binary/config, and started the replacement with both App
  Server sessions ready, no daemon error, and zero delivery backlog. Telegram
  Bot API readback confirmed the new `home`, `inbox`, `current`, `title`,
  `archive`, and `unarchive` commands, and accepted the HTML completion notice.

- 2026-08-26 CST Telegram session administration validation added prompt-title
  fallback with UUID/generic-placeholder protection, `/current`, write-through
  `/title` with in-place card refresh, confirmed current-session `/archive`, and
  App Server-backed `/unarchive` with ten-row cursor pagination and title-button
  restore. `/cancel` remains immediate and scoped to pending session creation.
  Targeted title/current/archive/pagination tests plus full `go test ./...`,
  `go build -buildvcs=false ./...`, and `git diff --check` passed. A real
  isolated-home App Server smoke also accepted `thread/list` with
  `archived=true` and returned the expected paginated response shape. Live
  Telegram readback remains required after the rollback-capable Windows cutover.

- 2026-08-25 CST single-foreground Telegram session validation made the current
  Telegram App Server list authoritative for `/threads`, replaced numbered Open
  rows with Unicode-safe clickable session titles, rejected cached Desktop-only
  threads, suppressed all background progress cards, de-duplicated compact
  completion/failed/cancelled/needs-input switch notices, and deleted the old
  non-terminal card when switching focus. Targeted foreground, stale-cache, and
  picker tests plus full `go test ./...`, `go build -buildvcs=false ./...`, and
  `git diff --check` passed. Live Telegram readback remains required after the
  rollback-capable idle-gated Windows cutover.

- 2026-08-25 CST Activity Card title hotfix added the App Server thread/task
  title as the primary row and moved Codex/state/duration to the secondary row,
  with a compact single-row fallback only while no title is available. Renderer
  escaping/fallback tests, observer integration coverage, full `go test ./...`,
  `go build -buildvcs=false ./...`, and `git diff --check` passed. Live Telegram
  readback remains required after the idle-gated hotfix restart.

- 2026-08-25 CST `/newchat` and `/newthread` two-step prompt validation passed
  targeted tests for arming, chat/topic isolation, cancellation, expiry, restart
  persistence, inline-command compatibility, and Telegram command-menu exposure.
  Full `go test ./...`, `go build -buildvcs=false ./...`, and
  `git diff --check` passed. The rebuilt Windows candidate also passed config
  readback plus a real isolated-home App Server `initialize` / `thread/list`
  smoke. Production Telegram readback remains required after idle-gated cutover.

- 2026-08-25 CST Telegram Activity Card pre-cutover validation passed targeted
  aggregator/lifecycle/renderer/photo tests, full `go test ./...`, Windows build,
  `git diff --check`, and a real isolated-Codex-home App Server smoke covering
  `initialize` plus `thread/list`. The isolated home returned zero threads while
  shared skills/plugins/packages, global config/rules, login, and the independent
  shared-memory store remained available. The production bot poller was not
  duplicated during shadow validation. A rollback-capable cutover is deferred
  until the current Telegram turn is terminal; real Activity Card readback is
  still required after the replacement daemon starts.

- 2026-08-20 CST Telegram card hierarchy validation rebuilt and restarted the
  local Windows daemon, then used operator-provided Telegram client readback to
  confirm the three-row structured header: bold thread title, emphasized role / status /
  duration, code-styled project plus short thread/turn metadata, and preserved
  Markdown response body rendering. The observed `interrupted` state belonged
  to the deployment-interrupted validation turn rather than a rendering fault.

This file captures validation nuances that are useful for agents and maintainers. It is not a list of release blockers.

For feature-to-test ownership, see `docs/testing/regression-map.md`.

## v0.5.0 control-plane architecture

2026-05-18 PDT: v0.5.0 control-plane architecture validation added ADR-019, Control Plane docs, internal control interfaces, App Server capability mapping, normalized event contracts, and notification severity policy. The slice was validated with targeted `internal/control`, `internal/appserver`, and `internal/daemon` tests, full `go test ./...`, `go build -buildvcs=false ./...`, `git diff --check`, and targeted secret/local scans. Live Telegram E2E was not required because Telegram-visible behavior was not changed.

## Runtime and documented UX contract

The public docs describe the intended observer/UI contract:

- `/observe all` moves one global observer target.
- `/observe off` disables passive monitoring.
- each turn normally has one activity card after an initial four-second typing period.
- raw tool/output events are aggregated into recent user-facing activities and
  non-terminal edits are limited to one every four seconds.
- completed, failed, cancelled, and input-required states edit the same card;
  long final content may continue in separate Result messages.
- the conversation emoji stays stable while textual state changes.
- routed Telegram photos become App Server `localImage` inputs without a receipt card.
- `/newchat` and `/newthread` collect a title and then a distinct plain-text
  prompt, retaining the pending stage for 15 minutes; `/cancel` clears that
  SQLite-backed chat/topic-scoped state, while the one-line command forms remain
  supported.
- short foreground terminal edits add one de-duplicated audible completion
  notice because Telegram edits alone do not notify.
- `Tools file` and `Get full log` are explicit on-demand exports.

When changing this behavior, update the ADRs and run both unit tests and a live Telegram E2E path if a bot token is available.

## App Server drift

`codex app-server` is the integration surface, but its thread/read payloads and live notifications can drift across Codex versions. Tests should cover snapshot normalization, plan prompts, tool rendering from `thread/read`, and fallback parsing.

Important areas to re-check after Codex upgrades:

- `agentMessage.phase` classification for commentary versus final answers.
- `status.activeFlags` values such as `waitingOnUserInput` and `waitingOnInput`.
- stale lifecycle snapshots where a latest turn has `final_answer` but still reports `inProgress`.
- `turn/steer` error text for stale turns, especially `no active turn to steer`.
- tool call shape in live notifications and `thread/read` snapshots.
- availability of `thread.raw_json.thread.path` for full-log exports.
- missing or null command/request/status fields that could render as literal `"<nil>"`.

## Stale active turns

`thread/read` status alone is not sufficient to decide whether Telegram input would start a parallel turn. The bridge normalizes a latest `final_answer` to completed state and clears the active turn before routing. If a steer attempt returns `no active turn to steer`, the bridge re-reads the thread and may start a new turn. Errors that imply an active or not-steerable turn still block fallback.

Regression tests for this area should cover:

- final-answer normalization from stale `inProgress`.
- no resurrection of SQLite `ActiveTurnID` after a terminal snapshot.
- fallback `turn/start` after `no active turn to steer`.
- no fallback for active-but-not-steerable failures.
- no duplicate global-observer panel for a marked Telegram-origin turn.

## Session lifecycle and interrupted drift

Live Telegram-origin turns can expose two independent forms of App Server drift:

- duplicate live event loops after daemon startup or repair when session lifecycle is not serialized.
- transient `interrupted` snapshots, including final-bearing snapshots, that later recover to `inProgress` or `completed` for the same turn.

Regression tests should cover:

- startup/reconcile/repair cannot create duplicate live subscriptions.
- stale old live loops cannot clear newer session state or trigger repair loops.
- Telegram-origin `interrupted` does not compact or render terminal during the grace window, even when partial tool/output or final evidence is already present.
- Telegram-origin active turns use App Server live `item/*` events for authoritative current tool visibility; `thread/read` remains the reconciliation source for completed/foreign state.
- explicit `/stop` accepts `interrupted` immediately.
- recovered turns clear the defer marker and continue normal live panel rendering.

## Routing precedence

The product contract remains:

1. explicit thread id in a command.
2. reply-to routed message.
3. armed one-shot steer or answer state.
4. bound thread for the current chat/topic.

Route correctness must be tested through persisted message routes and callback tokens, not by parsing rendered Telegram headers.

## Storage-backed tests

The runtime depends on SQLite through `modernc.org/sqlite`. Broad `go test ./...` coverage expects `go.sum` to be committed and the local Go toolchain to be available.

Validation commands:

```powershell
go test ./...
go build -buildvcs=false ./...
git diff --check
```

## Live Telegram validation

Bot API send/edit success is not enough for user-facing changes. For Telegram UI, routing, callbacks, Plan Mode, Details, Markdown formatting, or observer behavior, verify the rendered result with a real Telegram readback when possible.

## Nil-safe Telegram rendering validation

Literal `"<nil>"` in Telegram is treated as a rendering bug. It usually means App Server or rollout data omitted a field that Go code stringified with `%v`.

Validation expectations:

- unit tests cover nil-like map/slice extraction, command rendering, stale session-tail command suppression, summary Markdown rendering, App Server RPC id stringification, and snapshot string normalization.
- live validation must read edited Telegram messages, not only newly delivered messages.
- the checked-in public-safe harness `tests/live_e2e/telegram_readback_e2e.py` exercises sequential `pwd`, `date`, `printf`, a dedicated sleep-20 timing run, and a multi-command math run against a dedicated private test thread configured only through local env.
- when validating stale-command regressions manually, prefer one private test-thread turn that runs several safe shell commands sequentially; watch Telegram `MessageEdited` updates and verify the same Working card advances through recent activities without stale-command reversion.
- current command visibility comes from matching App Server events for the same
  `thread_id + turn_id` and is limited to one compact command under the current activity.
- while a run is active, the card shows elapsed time; its terminal header shows total duration.
- after a later turn completes, delayed live tool notifications from an earlier turn must not create a new observer panel or re-render stale tool/output content.
- raw Tool/Output and empty-placeholder labels must not appear in the default card.
- Details view should be checked during E2E when the button is available. For multiple completed runs in one thread, opening `Details` and pressing `Back` on an older Final Card must keep that older card bound to its original turn; stale or mismatched Details callbacks must not render the latest run.
- Tool-only turns with no commentary and no output must still show completed command/status in Details under `Tool activity`.
- do not commit Telegram sessions, target thread ids, raw message ids, logs, env files, or screenshots.

Latest local validation note:

- 2026-05-07 PDT macOS v0.4.0 service installer validation built the CLI and tray app, built a local darwin/arm64 `.pkg` dry-run, verified package contents, and confirmed admin installation is blocked in this non-interactive session without sudo credentials. A non-interactive `ctr-go service install` temp-home smoke wrote `config.env` with `0600`, generated a LaunchAgent with only `CTR_GO_CONFIG`, and did not leak the dummy token. The real service setup path then ran the friendly wizard over the existing private config, preserved proxy runtime env in `config.env`, installed the user LaunchAgent, and started the daemon. `launchctl` showed the plist environment limited to `CTR_GO_CONFIG`; Telegram `/status` readback showed the daemon ready with live and poll App Server sessions connected. Live `newchat_folder` and `tool_only_sleep_details` E2E passed through the service-managed daemon, including real Chat folder creation, `/newthread` no-cwd behavior, live `Current tool:` during `sleep 10`, Details `Tool activity`, and Tools file export. Service lifecycle smoke covered start/stop/start plus login enable/disable, and stderr token URL redaction was verified after an early Telegram startup timeout exposed the risk.
- 2026-05-07 PDT macOS v0.3.0 distribution validation built local release archives for macOS, Linux, and Windows through `scripts/build-release.sh`, then extracted the darwin/arm64 archive and ran `ctr-go init` plus `ctr-go status` against a temporary config/home path. The binary reported `v0.3.0`, wrote the config with `0600` permissions, read config-file values without token env vars, and did not print the dummy Telegram token in init/status output. Targeted config/CLI tests, full repository tests, and `go build -buildvcs=false ./...` passed before final release checks.
- 2026-05-06 PDT macOS Plan Mode reset E2E rebuilt and restarted the LaunchAgent daemon, then ran the checked-in `plan_mode_reset` live case through MTProto readback. The case created a disposable thread, started Plan Mode, observed a Plan-like Final Card with `Turn off Plan`, clicked it, and verified the next normal `sleep 5` request executed with `Current tool:` before reaching `[Final]`. It repeated the reset path with `/stop <thread>` while the thread was idle; `/stop` returned an idle response and the following normal `sleep 5` request also executed. No private thread/message ids, raw logs, or local env values are recorded here.
- 2026-05-06 macOS v0.2.5 release validation rebuilt and restarted the LaunchAgent daemon after adding the notification contract, tool-only Details preservation, and explicit Default Mode escape hatch. Targeted App Server/daemon/Telegram tests, full repository tests, build, diff check, Python harness compile, and targeted secret/local scan passed before tagging. The release uses the prior checked-in `tool_only_sleep_details` and `notification_contract` live Telegram readback evidence for the user-visible message lifecycle.
- 2026-05-05 PDT macOS tool-only sleep Details E2E rebuilt and restarted the LaunchAgent daemon, then ran the checked-in `tool_only_sleep_details` live case through MTProto readback against a private execution-mode test thread. `[Tool]` showed `Current tool:` for a single `sleep 10` command before completion, then `Last completed tool:` before Final cleanup. Opening Details on the new Final Card showed `Tool activity` with the completed command/status, `Tool on` kept the same tool visible, and `Tools file` exported the command despite empty output. A follow-up `notification_contract` smoke passed on the same execution-mode contour.
- 2026-05-05 PDT macOS notification-contract E2E rebuilt and restarted the LaunchAgent daemon, then ran the checked-in `notification_contract` live case through MTProto readback. The case observed `New run`, live `[commentary]`/`[Tool]`/`[Output]`, a new `[Final]` card with a different message id, deletion of the old live commentary card, and Details/Back editing the new Final card. The Plan step reached a routeable `[Plan]` card; the live App Server shape did not expose structured choice buttons, so the harness recorded the documented fallback and stopped the waiting Plan turn.
- 2026-05-05 PDT macOS newchat folder E2E rebuilt and restarted the LaunchAgent daemon, then ran the checked-in `newchat_folder` live case through MTProto readback. `/newchat` created a dated Chat folder under the configured Chats root, reached `[Final]`, and `/context` was bound to that generated cwd. `/newthread` reached `[Final]` without creating a Chat folder under the configured Chats root; App Server reported the daemon default cwd for that thread. No literal `"<nil>"`, visible interrupted state, or false parallel-turn rejection appeared.
- 2026-05-05 PDT macOS command-menu smoke rebuilt and restarted the LaunchAgent daemon, then read Telegram Bot API `getMyCommands` for the private operator bot without printing the token. The registered command list included `newchat` alongside the existing menu commands.
- 2026-05-05 PDT macOS Details binding regression E2E rebuilt and restarted the LaunchAgent daemon, then used MTProto readback against a private test thread. The checked-in `details_binding` live case created two completed Telegram-origin `/reply` runs in the same Codex thread. Opening `Details` on the older Final Card showed only the older run commentary, `Tool on` showed only the older tool/output, `Tools file` downloaded older-run details, `Back` restored the older Final Card in the same message, and the newer Final Card remained unchanged. No latest-run duplication, stale Details fallback, or literal `"<nil>"` appeared.
- 2026-05-04 PDT macOS Projects/Chats label live readback rebuilt and restarted the LaunchAgent daemon, then used MTProto readback against the private operator bot. `/projects` rendered named project buttons in `N. Project name` format, named Chat preview buttons in `Chat N. Thread name` format, project rows with `last thread:`, and no visible `key:` rows. Opening a named project showed the project menu with `New thread`; `Open Chats` showed named Chat buttons with no `New thread` action; selecting a Chat opened/bound it as confirmed by `/context`.
- 2026-05-04 PDT macOS Projects/Chats live readback rebuilt and restarted the LaunchAgent daemon, then used MTProto readback against the private operator bot. `/projects` rendered normal projects newest-first with `Latest Chats` previews and no `Documents/Codex` cwd entries as normal project rows. `Open Chats` edited the same menu into a paginated `Chats` list with no `New thread` action. Selecting the first Chat opened/bound its single thread as confirmed by `/context`. `/newchat <prompt>` started a new App Server thread without a cwd parameter, reached `[Final]`, and a plain follow-up routed to the newly bound thread. No literal `"<nil>"` appeared.
- 2026-05-04 PDT macOS live current-tool priority regression E2E rebuilt and restarted the LaunchAgent daemon, then used MTProto readback against a private test thread. The new `current_tool_priority` case asked a Telegram-origin `/reply` run to execute two separate long-running shell commands that print progress lines while they work. `[Tool]` showed the first command as current, later the first completed progress output, then the second command as `Current tool:`; after the second command became current, `[Tool]` did not revert to the older completed first command before settling on the second completed command. `[Final]` included `Run duration`; no literal `"<nil>"`, stale-command revert, false parallel-turn rejection, or visible interrupted state appeared.
- 2026-05-04 PDT macOS v0.2.0 live-event refactor E2E rebuilt and restarted the LaunchAgent daemon, then used MTProto readback against a private test thread. `sleep20_timing` showed `Current tool:` for `sleep 20; printf ...` before completion, then `Last completed tool:`, `[Output]`, and `[Final]` with run duration. `multi_tool_current` showed two separate live current tool transitions and all three completed tool/output transitions. A follow-up lifecycle smoke exposed an upstream App Server/tool-run failure (`write_stdin failed: stdin is closed for this session`) on one intentionally strict sequential-command prompt; after preserving live current state across tool-less same-turn `thread/read`, a fresh `sleep20_timing` retry passed with no `<nil>`, stale command, false parallel-turn rejection, or visible non-final interrupted state.
- 2026-05-03 PDT macOS create-thread live readback verified `/projects -> project menu -> New thread -> first prompt -> [Final]` and a plain follow-up routed to the newly bound thread. The same pass exposed a synthetic Plan fallback bug: a later Plan turn with no structured question rendered the previous turn preview as the `[Plan]` prompt. After the normalizer fix, the Plan-only live retry rendered `Input required.` with no stale previous prompt and no choice buttons outside `[Plan]`.
- 2026-05-03 PDT macOS live readback found a stale Plan button regression: a newer `[commentary]` card for turn `...efda` displayed structured answer buttons whose callback routes still targeted older Plan turn `...eea2` / item `...a08f`. Sanitized daemon evidence showed the older turn repeatedly alternating `interrupted` and `inProgress` with `waiting_reply=true`, then a newer turn entering `inProgress`; the pending `user_input` row remained attached to the older turn and must be filtered by turn before rendering summary buttons.
- 2026-04-30 PDT macOS live complex `/reply` E2E reproduced a transient partial `interrupted` snapshot with tool/output evidence before final completion. After extending the terminal gate to defer non-final Telegram-origin `interrupted`, the same E2E passed: sequential command updates stayed visible, then a multi-command number-theory task created a temporary helper and ran four Python range commands before reaching `[Final]` with `COUNT=2034 SUM=115514223`; no visible interrupted state, literal `"<nil>"`, stale command, or input rejection appeared.
- 2026-04-30 PDT macOS live logging-flags E2E used MTProto readback against a private test thread after rebuilding and restarting the daemon. It verified sequential `pwd`, `date`, `printf 'alpha\nbeta\n'`, and `sleep 20; printf 'slow-command-done\n'` tool updates, with the slow command visible in `[Tool]` about 20 seconds before `[Output]`; a separate `/reply` math run reached `[Final]` with the expected answer and no visible interrupted state. Daemon diagnostics remained present with default logging flags.
- 2026-04-30 PDT macOS live stale-command E2E used MTProto readback of `MessageEdited` updates for one private test-thread turn with `pwd`, `date`, `printf 'alpha\nbeta\n'`, and `sleep 20; printf 'slow-command-done\n'`. `[Tool]` showed the slow command in progress about 20 seconds before `[Output]` contained `slow-command-done`; no literal `"<nil>"` or stale session-tail command appeared.
- 2026-04-29 macOS live nil-guard E2E completed all three scenarios and found no literal `"<nil>"` in edited New run, summary/Final, Tool, Output, or Details messages after the sanitizer change.

## Turn lifecycle live E2E

The checked-in public-safe harness `tests/live_e2e/telegram_readback_e2e.py` validates Telegram-origin lifecycle behavior. It must use MTProto readback, not Bot API polling, and it must require `CODEX_TG_LIVE_E2E=1` plus `CODEX_TG_E2E_THREAD_ID` so it never defaults to the current operator thread.

Acceptance checks:

- sequential command run reaches `[Final]`.
- Telegram-origin slow command becomes visible as `Current tool:` before `[Output]` contains its completion text.
- complex `/reply` math run uses multiple shell commands and reaches `[Final]` with the expected aggregate answer.
- Telegram readback contains no false parallel-turn warning, literal `"<nil>"`, stale known command, or false visible `Status: interrupted`.
- optional daemon log correlation for the scenario window contains no premature interrupted terminal before recovery/expiry, no input rejection, and no `telegram_render_contains_nil`.

Do not commit Telegram user sessions, chat/thread ids, raw message ids, raw logs, env files, or screenshots.
