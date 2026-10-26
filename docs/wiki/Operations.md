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

For planned restarts, use this order:

1. Check the active run state in Telegram or with `go run ./cmd/ctr-go status`.
2. Wait for the active Telegram-originated turn to finish.
3. Rebuild or reinstall the binary.
4. Restart the daemon with `ctr-go service restart` or the tray menu.

The supported CLI rejects `service start`, `stop`, `restart`, `install` and
`uninstall` inside a hosted task (`CTR_GO_HOSTED_TASK=1`), before lifecycle or
configuration mutations. Read-only status remains available. Run lifecycle
maintenance from an independent process after the current task finishes.
External restart retains launchd registration and uses `kickstart -k`; a failure
does not unload the job. This does not provide task draining or automatic task
continuation after restart.

Do not run `launchctl bootout`, `kill` or old stop-and-reload migration scripts
from the task being maintained. With full access, arbitrary commands can bypass
the CLI guard, so the runtime instructions also explain this boundary. Permission
migrations that are already applied should return success without restarting.
More invasive designs, such as a separate App Server broker process, require a
separate ADR.

## Windows Codex Runtime Failover

Windows enables managed runtime mode by default. On first daemon start,
codex-tg copies the complete configured/Desktop CLI bundle under
`~/.codex-tg/codex-runtime`. Later Desktop updates can remove their old
content-addressed directory without breaking Telegram.

The daemon checks for a newer complete local bundle every six hours. Promotion
affects only the next daemon start. That first start must connect both live and
poll App Server sessions; failure swaps the active pointer back to the previous
confirmed bundle and retries once. All probes and child processes are windowless.

Use `CTR_GO_CODEX_RUNTIME_MANAGED=false` only when an external package manager
already provides a stable complete bundle. `ctr-go status` reports the managed
binary after the private runtime has been initialized.

`ctr-go runtime status` inspects the active/previous selection,
`ctr-go runtime sync` performs an immediate non-disruptive discovery and stage,
and `ctr-go runtime rollback` swaps the pointers for the next daemon start.

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
Server and inherits the same SQLite-backed settings as conversation turns;
operators view them with `/m` and edit them with `/sm`. Task definitions do not
select an independent model. Results
and input/approval requests use the normal Telegram observer.
Schedules use local-time RRULEs with hourly, daily, weekly, and monthly
frequencies. Monthly rules use `BYMONTHDAY=1..31`; a date absent from a month is
skipped. A successful run that returns only `DONT_NOTIFY` is recorded without a
Telegram push.
If a task does not start, check `automation.scheduler.last_tick_at` and
`automation.scheduler.last_error` in `ctr-go doctor`, verify the live App Server
is ready, and verify an observer target can be resolved. A task edited after its
scheduled time intentionally waits for the next matching slot.

When upgrading from ADR-022, move only Telegram-created `cron` definitions out
of the Desktop directory, update `CTR_GO_AUTOMATIONS_DIR`, and leave Desktop
heartbeat tasks in place. Do not point both clients at the same task store.
