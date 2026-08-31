# Contract Matrix

Python oracle: `..\codex-telegram-remote`

This file now serves two purposes:

- baseline behavior imported from the Python oracle
- target Telegram observer/UI v2 deltas that the Go runtime is expected to adopt
- v0.5 Codex Control Plane contracts that future adapters should consume

## Commands

- `/start`
- `/home`
- `/help`
- `/inbox`
- `/threads`
- `/projects`
- `/show <thread>`
- `/bind <thread>`
- `/reply [--plan] <thread> <text>`
- `/plan <thread> <text>`
- `/plan <text>`
- `/plan_mode <thread> <text>`
- `/plan_mode <text>`
- `/settings`
- `/model`
- `/effort`
- `/new <project-key-or-number> <prompt>`
- `/newchat <prompt>`
- `/newthread <prompt>`
- `/newchat` or `/newthread`, followed by a plain-text title and then a distinct text-or-photo prompt; `/cancel` aborts either pending stage
- `/context`
- `/whereami`
- `/observe all|off`
- `/status`
- `/release`
- `/repair`
- `/stop [thread]`
- `/approve <request_id>`
- `/deny <request_id>`

## Aliases and adjacent commands

- `/whereami` is an alias of `/context`
- `/models` is an alias of `/model`
- `/reasoning` and `/reasoning_effort` are aliases of `/effort`
- `/codex_settings` is an alias of `/settings`
- `/away` and `/back` exist in the Python product surface but are not part of the minimal Go cutover slice yet

## Local configuration and distribution

- `ctr-go init` creates a private local config file at `~/.codex-tg/config.env` by default.
- `ctr-go service install` is the macOS service-first setup path; it can prompt
  interactively or receive all important values through flags.
- macOS service lifecycle commands are `ctr-go service start|stop|restart|status|enable-login|disable-login|uninstall`.
- `CTR_GO_CONFIG` points at an alternate config file.
- `CTR_GO_AUTOMATIONS_DIR` explicitly enables the Codex Desktop Scheduled task
  definition bridge; existing configs without it stay disabled.
- Config precedence is explicit environment variables, then config file values, then built-in defaults.
- Config files use simple `.env` style `KEY=VALUE` entries; comments and quoted values are supported, but shell expansion is not.
- Runtime proxy env can be stored in the private config and applied after
  startup; LaunchAgent plists still carry only `CTR_GO_CONFIG`.
- `status`, `doctor`, daemon logs, init summaries, service summaries, LaunchAgent plists, and tray surfaces must not print Telegram bot tokens in full.
- Official GitHub Release assets include `ctr-go` archives for macOS, Linux, and Windows, macOS `.pkg` artifacts, and `SHA256SUMS`.

## Codex Control API Contract

The first implementation target is internal Go interfaces, not a public HTTP
API. Future router-agent or voice adapters may consume the same contract through
a local loopback or unix-socket API after a separate ADR.

Thread lifecycle:

- list and search threads, including cwd-scoped queries when App Server supports them
- read a thread with or without full turns
- start, resume, fork, rename, archive, unarchive, compact, and rollback threads
- keep `thread_id` as the durable identity across every adapter
- detect unavailable App Server capabilities instead of assuming every method exists

Turn lifecycle:

- start a turn with text input and optional model, reasoning, cwd, approval, sandbox, and collaboration-mode settings
- steer an active turn only when the expected turn id matches
- interrupt an active turn by `thread_id + turn_id`
- answer user-input requests and approval requests through the originating App Server request id
- preserve existing stale-active recovery rules before falling back to a new `turn/start`

Event subscription:

- normalize App Server lifecycle, tool, final, approval, and input events before adapters see them
- treat `thread/read` snapshots as durable reconciliation state
- keep App Server live events as the only live tool/output/final source; session JSONL remains export-only
- expose enough ids for adapters to route replies, approvals, Details, and notifications safely

Skills and ecosystem:

- list available Codex skills by cwd when supported
- read plugin skill metadata when supported
- inspect MCP server status, app list, hooks list, and config state when supported
- prefer Codex-native Skills, Hooks, and Automations over duplicate custom formats

Scheduled tasks:

- new and resumed Telegram threads receive a private stdio
  `automation_update` MCP tool only when `CTR_GO_AUTOMATIONS_DIR` is configured
- the tool reads and writes native Codex automation definitions; it does not
  introduce a second task format or scheduler
- Telegram-created schedules are standalone local `cron` tasks; `heartbeat`
  is rejected because Telegram thread ids are not Desktop thread ids
- Codex Desktop owns schedule execution, run history, UI, and notifications and
  must be running for local tasks
- Scheduled run output is not synthesized as a Telegram runtime turn
- task ids and paths are validated, unknown native TOML fields survive updates,
  and automation prompts must not contain credentials

Notifications:

- classify normalized events as `urgent`, `normal`, `silent`, or `digest`
- urgent examples: approval needed, user input needed, run failed, security/sandbox denial
- normal examples: final answer, high-value run start when enabled by adapter policy
- silent examples: progress deltas, tool output deltas, menu/callback responses
- digest examples: low-priority automation summaries or scheduled findings

Adapter routing:

- adapters must not own Codex identity
- Telegram message ids, voice sessions, tray actions, and future HTTP requests are adapter context
- control-core operations route by durable Codex ids and explicit adapter-supplied intent

## Telegram Adapter Contract

- Global observer monitoring is default-on when an operator target can be resolved automatically.
- `/observe all` moves the single global observer target to the current chat/topic.
- `/observe off` disables global background monitoring.
- The observer target model is no longer additive `main DM + extra feeds`.
- `/start` and `/home` open the session hub. Home shows the foreground state and
  a bounded detail list plus total count for other running sessions, using the
  reconciled local thread/snapshot cache rather than a blocking navigation-time
  App Server request. `/inbox` is a durable per-target queue of background
  terminal and needs-input items; switching clears an item.
- The observer surface is centered around a summary panel keyed by `(chat, project, thread)`.
- The summary panel owns `Stop` and `Steer`.
- Raw tool/output events are aggregated into the summary panel and do not create
  separate default messages.
- The first four seconds use `typing`; surviving turns create one Working card,
  and active edits have a four-second minimum interval.
- Final, failure, cancellation, and input-required states edit the same activity
  card. Long final content may continue in separate `Codex · 结果` messages.
- Plan Mode / waiting-input states keep the summary card routeable.
- Plan buttons are structured-only: they come from Codex
  `choices/options/suggestions/responses`, never from bridge heuristics.
- Telegram-originated Plan Mode starts use App Server `turn/start` with `collaborationMode.mode = plan`; prompt wording alone is not Plan Mode.
- If a thread remains in Plan Mode, `退出 Plan` on the Plan Final Card and `/stop <thread>` set a one-shot local reset; the next ordinary Telegram-originated `turn/start` for that thread uses `collaborationMode.mode = default` and then clears the reset.
- `/model` and `/effort` are button menus backed by SQLite daemon state for Telegram-started collaboration-mode model settings.
- After a model or reasoning-effort selection, the edited settings message removes inline choice buttons.
- `/projects` groups cached non-Chat projects by normalized `cwd`, sorts projects by latest cached thread activity, shows latest Codex UI Chat previews, opens full Chats pagination through `Open Chats`, and never accepts arbitrary filesystem paths from Telegram.
- `/projects` buttons show meaningful labels (`N. Project name`, `Chat N. Thread name`); internal project keys are not rendered in the menu, and project rows show `last thread:`.
- Cached threads under generic `Documents/Codex` paths or the configured `CTR_GO_CODEX_CHATS_ROOT` are treated as single-thread `Chats`; selecting a Chat opens and binds that thread and does not offer project `New thread`.
- `New thread` creates a one-shot state; the next text or Telegram-photo message starts a new App Server thread in the selected project cwd and uses its structured inputs as the first prompt.
- `/newchat <prompt>` creates a dated Chat folder under the configured Chats root, calls App Server `thread/start` with that cwd, and uses the prompt as the first turn.
- `/newthread <prompt>` starts a new App Server thread without a Telegram-selected cwd parameter and uses the prompt as the first turn. It must not create a Chat folder; App Server may still attach the daemon default cwd.
- When either command omits `<prompt>`, the bridge persists a 15-minute chat/topic-scoped title-then-prompt state. The first plain-text message supplies an explicit App Server title and the second supplies the first turn prompt, including an optional Telegram `localImage`; a photo while the title is pending is not routed elsewhere. `/cancel` clears either stage, and restart does not lose it.
- `/plan <text>` and `/plan_mode <text>` use reply route, armed state, or current binding when the first token is not a known or UUID-like thread id.
- Synthetic polling prompts without `request_id` are answered with `turn/steer`, then `turn/start` if the turn is already unavailable.
- Replies to active turns steer the active turn. If steering is rejected while the thread still looks genuinely active, the bridge must not create a parallel `turn/start`; stale-active errors such as `no active turn to steer` are handled by ADR-012 and may fall back to a new `turn/start` after re-read.
- `/bind` and `在 TG 中继续` load and retain the Telegram live writer with
  `thread/resume`; binding also changes the foreground session. Observer-only
  tracking remains read-only.
- Bound writers are reacquired after repair/restart unless a persisted manual-release marker is present.
- Explicit Telegram input resumes its target just before the write. An active-writer conflict from another connection in the isolated Telegram runtime is returned immediately; the input is not queued and no parallel turn is started.
- `/release` and `释放空闲写入权` fail closed while any thread owned by the current Telegram live-session generation is active, waiting, or unverifiable. When all are idle they persist manual-release markers and replace only the live session; polling remains connected and cards return to `在 TG 中继续`.
- Five minutes without an allowed Telegram message or callback invokes that same fail-closed session release. Allowed Telegram activity resets the timer, including when it races with an automatic release check.
- Activity cards carry visual identity. The leading emoji is the stable
  conversation marker, followed by Codex and a textual state; short `T:`/`R:`
  ids are low-weight bottom metadata.
- Emoji markers are identity hints, not state icons; route correctness remains
  based on DB message routes and callback tokens.
- Full `thread_id` and `turn_id` are exposed through `/context` and the
  `查看会话 ID` summary/final action; compact `T:`/`R:` chips are not routing authority.
- `/title` marks its value as user-owned, so automatic runtime refreshes cannot
  replace it. `/archive` blocks active/input-waiting sessions and requires
  confirmation only for an archivable session. Restore success offers a switch action.
- On Windows with affected App Server versions, codex-tg tracks resumed threads
  in the current live generation. Archiving one recycles that generation only
  after every owned writer is confirmed idle, delegates the archive to the
  fresh App Server, then restores non-target writers. App Server still owns the
  archive move and state transition.
- Raw events are de-duplicated and prioritized into at most three recent
  activities. Fast incidental tools usually contribute only to the operation count.
- Telegram photos on routed threads or as a pending new thread's first prompt
  use the largest available size and App Server `localImage`; they do not create
  a separate receipt card.
- Private Telegram photo inputs remain readable for 30 minutes after dispatch
  because App Server ingestion may be asynchronous; scheduled cleanup removes
  them, and matching files older than 24 hours are treated as stale.
- Outside fenced code blocks, GitHub-style Markdown pipe tables render as
  per-row Telegram list records rather than raw aligned text. The first column
  is the record title and remaining columns are vertically labeled fields.
- Telegram-visible text must never render literal `"<nil>"`. Missing, null, empty, or nil-like App Server fields are treated as absent and must be cleaned before Markdown/entity conversion.

## Callback / button surface from the oracle

Navigation/edit-in-place callbacks:

- `nav_projects`
- `nav_all_chats`
- `nav_active`
- `nav_threads_page`
- `nav_projects_page`
- `nav_project_threads_page`
- `pick_project`
- `show_thread`
- `show_context`

State-changing callbacks:

- `bind_here`
- `follow_here`
- `observe_all`
- `reply_hint`
- `stop_turn`
- `approve`
- `approve_session`
- `deny`
- `cancel`

Target v2 callback surface:

- `show_thread`
- `bind_here`
- `stop_turn`
- `steer_turn`
- `get_full_log`
- `answer_choice`
- `observe_all`
- `observe_off`
- `settings_overview`
- `settings_model_menu`
- `settings_reasoning_menu`
- `settings_model_set`
- `settings_reasoning_set`
- `get_thread_id`
- `turn_off_plan`

## Routing precedence

From Python tests and router behavior:

1. explicit thread id from command
2. reply-to Telegram message route
3. current thread binding

Additional route rules:

- `/show` and `/bind` without an explicit thread id must resolve reply-route first
- route precedence stays unchanged even after the observer/UI v2 changes
- target v2 no longer assumes a dedicated read-only observer-only chat
- free-text routing still needs an unambiguous target even if the current chat also receives global observer panels
- reply-to `[Plan]` routes before binding and carries `thread_id`, `turn_id`, and `request_id` when available
- real `request_id` Plan answers use App Server server-request response; synthetic Plan answers use `turn/steer`
- `/reply --plan`, `/plan`, and `/plan_mode` carry an explicit Plan Mode start intent when they create a new turn
- Hidden `/reply --default` and `/default` fallback paths may still carry an explicit Default Mode start intent, but they are not advertised in the public command menu.
- `退出 Plan` and `/stop` carry a one-shot Default Mode reset intent for the next ordinary turn, not for the current callback/stop action itself.

## Observer targets

- Oracle baseline:
  - implicit main DM when exactly one allowed user exists
  - explicit observer targets from `/observe all`
  - explicit observer targets do not replace the implicit main DM
- Target v2:
  - one global observer target
  - default-on when the target can be resolved automatically
  - `/observe all` moves the target
  - `/observe off` disables monitoring

## Minimal observer event kinds

- `turn_started`
- `tool_activity`
- `thread_updated`
- `final_answer`
- `turn_completed`
- `turn_failed`

Observer/UI v2 presentation contract:

- run notice:
  - appears before `[User]` and summary/tool/output for new runs
  - carries source markers, source mode, and route metadata, but not run status
  - is deleted best-effort after finalization
  - uses normal Telegram notification only when `CTR_GO_NOTIFY_NEW_RUN` is enabled
- user notice:
  - appears after `New run` for non-Telegram runs inside the TG runtime and before summary/tool/output
  - remains after finalization as the historical request marker
  - may start as a placeholder and edit into the actual prompt
- summary-panel update:
  - carries project/thread source markers
  - owns live run status while active
  - carries action buttons such as `Stop` and `Steer`
  - is sent silently and deleted best-effort after finalization
- tool/output message:
  - carries source markers
  - carries no buttons
  - is deleted best-effort after finalization
- final-answer message:
  - carries source markers
  - carries on-demand `Получить полный лог`
  - is sent as a new message with a normal Telegram notification
  - becomes the panel summary message id for Details/Back callbacks
  - contains final answer/status without replaying completed commentary/tool/output transcript
  - exposes completed tool-only turns through Details as `Tool activity`

Minimal event payload expected by the Telegram layer:

- `event_id`
- `kind`
- `thread_id`
- `project_name`
- `thread_title`
- `text`

Optional event payload fields:

- `status`
- `turn_id`
- `item_id`
- `request_id`
- `needs_reply`
- `needs_approval`

Plan prompt payload fields:

- `prompt_id`
- `source`
- `thread_id`
- `turn_id`
- `item_id`
- `request_id`
- `question`
- `options`
- `fingerprint`

## Acceptance scenarios

- global observer is active by default when one operator target exists
- `/observe all` moves the observer target to the current chat/topic
- `/observe off` disables global monitoring
- `/status` must show readiness, transport, queue, tracked thread count, and current routing
- `/context` must describe the active tuple of chat/project/thread or the lack of one
- polling fallback must emit progress/final/completion for poll-discovered TG-runtime threads
- stale live-only assumptions must not suppress polling fallback
- repair must recreate app-server sessions, resume bound writers unless manually released, and keep observer-only tracking read-only
- observer delivery must remain durable across daemon restart
- summary panels must be stable per `(chat, project, thread)` instead of spamming a new actionable message for every event
- waiting Plan prompts must be visible as `[Plan]` messages and answerable by Telegram Reply
- Plan answer buttons must stay scoped to their Plan turn; a stale pending input from an older turn must not be attached to a newer `[commentary]` card
- late poll-discovered `[User]` prompts must edit the existing placeholder, not append below live trio messages
- duplicate live+poll sync must not create multiple `[Plan]` cards for the same prompt fingerprint
