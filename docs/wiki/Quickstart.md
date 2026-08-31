# Quickstart

## 1. Create a Telegram Bot

Create a bot with BotFather and keep the token private.

## 2. Clone And Initialize

This fork release is source-only:

```powershell
git clone https://github.com/Amadeus1P048596/codex-tg.git
cd codex-tg
go run ./cmd/ctr-go init
go run ./cmd/ctr-go doctor
```

`ctr-go init` writes `~/.codex-tg/config.env` and defaults
`CTR_GO_CODEX_HOME` to `~/.codex-tg/codex-home`. This keeps Telegram sessions,
state databases, writer locks, caches, and App Server processes separate from
Codex Desktop. Provision Codex authentication in that private home before
starting the daemon. The wizard also proposes `~/.codex/automations` for the
optional Desktop Scheduled tasks bridge; existing configs must add
`CTR_GO_AUTOMATIONS_DIR` explicitly.

For manual setup on any OS:

```powershell
ctr-go init
ctr-go doctor
ctr-go daemon run
```

Use `CTR_GO_CONFIG` when you want a different config path. Explicit environment
variables override config file values. On macOS, a source build also provides
`ctr-go service install`; its wizard writes the same dedicated runtime home and
preserves proxy env needed by the LaunchAgent while keeping the plist limited to
`CTR_GO_CONFIG`.

## Environment-Only Setup

```powershell
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_GO_ALLOWED_USER_IDS = "<telegram-user-id>"
$env:CTR_GO_DEFAULT_CWD = "C:\Users\you\Projects\Codex"
$env:CTR_GO_CODEX_CHATS_ROOT = "C:\Users\you\Documents\Codex"
$env:CTR_GO_CODEX_HOME = "C:\Users\you\.codex-tg\codex-home"
$env:CTR_GO_AUTOMATIONS_DIR = "C:\Users\you\.codex\automations"
# Optional: set to "off" to keep New run visible but silent.
$env:CTR_GO_NOTIFY_NEW_RUN = "on"
```

## Build From Source

Source builds require Go 1.26 or newer.

```powershell
go run ./cmd/ctr-go init
go run ./cmd/ctr-go doctor
go run ./cmd/ctr-go daemon run
```

## 3. Enable Observer

In Telegram:

```text
/start
/observe all
/threads
/projects
```

Start or continue a thread from Telegram. A CLI explicitly pointed at the same
Telegram `CODEX_HOME` may also create work for the poll session to discover, but
Windows Codex Desktop history is intentionally outside this runtime.
Only `New run`, `[Plan]`, and `[Final]` use normal Telegram notifications. Live progress cards, menus, and exports are sent silently.

Use `/plan` or `/reply --plan` for Plan Mode. If a thread remains in Plan Mode,
press `Turn off Plan` on the Plan Final Card, or use `/stop <thread>`, then send
the next normal prompt. The bridge applies App Server Default Mode to that next
ordinary turn.

To start a new thread from Telegram, open `/projects`, choose a project, press
`New thread`, then send the first prompt as the next message. The selected
project must already exist in the cached Codex thread list.

Codex UI Chats stored under `Documents/Codex` appear under the `Chats` section
instead of as normal projects. Use `Open Chats` for the full paginated list.
Send `/newchat`, then send the first prompt as the next message, to create a new
Codex UI Chat under `Documents/Codex/<date>/<prompt-slug>`. Send `/newthread`
for the same two-step interaction without choosing a project or creating a Chat
folder. `/newchat <prompt>` and `/newthread <prompt>` remain available as
one-line shortcuts, and `/cancel` cancels either pending flow. App Server may
still attach the daemon default cwd to `/newthread` threads.
