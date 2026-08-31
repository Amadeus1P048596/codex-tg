# Operations

## Status

```powershell
ctr-go status
ctr-go service status
```

In Telegram:

```text
/status
/context
```

## Repair

```powershell
ctr-go repair
```

In Telegram:

```text
/repair
```

## macOS Service

```powershell
ctr-go service install --start --start-at-login
ctr-go service status
ctr-go service stop
ctr-go service start
ctr-go service restart
ctr-go service disable-login
ctr-go service enable-login
ctr-go service uninstall --keep-config
```

The service is a user LaunchAgent. Its environment contains only
`CTR_GO_CONFIG`; tokens and user ids stay in the local config file.
Proxy variables needed for Telegram/Codex network access are also preserved in
the private config and applied by `ctr-go` after startup, instead of being
written directly into the LaunchAgent plist.

## Restart Safety

Avoid forced daemon restarts while a Telegram-originated run is active. The daemon owns the local App Server stdio session used by that run; killing the daemon closes that transport and can make the active turn appear as `interrupted`.

Until a safe restart command exists, prefer this order:

1. Check the active run state in Telegram or with `go run ./cmd/ctr-go status`.
2. Wait for the active Telegram-originated turn to finish.
3. Rebuild or reinstall the binary.
4. Restart the daemon with `ctr-go service restart` or the tray menu.

Future restart work should implement a drain/guard path that refuses or delays restart while Telegram-origin turns are active. More invasive designs, such as a separate App Server broker process or reattaching to a turn after daemon death, require a separate ADR.

## Common Issue

Telegram `409 Conflict` means another process is polling the same bot token. Stop the other consumer before starting `codex-tg`.

## Local Config

`ctr-go init` and `ctr-go service install` write `~/.codex-tg/config.env` by
default. Set `CTR_GO_CONFIG` to use another path. Environment variables override
values from the config file, which lets LaunchAgent/systemd/manual deployments
keep their existing overrides.

## Scheduled Tasks

`CTR_GO_AUTOMATIONS_DIR` defaults to the private
`~/.codex-tg/automations` directory. Keep it separate from Codex Desktop's
`~/.codex/automations`; `ctr-go status` reports the active path.

The MCP adapter manages standalone cron definitions while the daemon polls and
claims due time slots. Every run starts a new thread in the TG-private App
Server, so results and input/approval requests use the normal Telegram observer.
If a task does not start, check `automation.scheduler.last_tick_at` and
`automation.scheduler.last_error` in `ctr-go doctor`, verify the live App Server
is ready, and verify an observer target can be resolved. A task edited after its
scheduled time intentionally waits for the next matching slot.

When upgrading from ADR-022, move only Telegram-created `cron` definitions out
of the Desktop directory, update `CTR_GO_AUTOMATIONS_DIR`, and leave Desktop
heartbeat tasks in place. Do not point both clients at the same task store.
