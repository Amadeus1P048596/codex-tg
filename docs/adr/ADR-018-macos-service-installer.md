# ADR-018: macOS Service Installer And Tray Control

- Status: accepted
- Supersedes: ADR-017 non-goal "No automatic OS service installation" for macOS only.

## Context

Release archives made `codex-tg` easier to try, but operators still had to wire
the daemon into macOS startup by hand. That is too much setup friction for a
local-first Telegram control loop that is meant to run continuously.

## Decisions

- `ctr-go service install` owns macOS first-run service setup.
- Setup has a friendly interactive CLI wizard and equivalent non-interactive
flags for scripted installs.
- The daemon runs as a user LaunchAgent, not as a root/system LaunchDaemon.
- The LaunchAgent receives only `CTR_GO_CONFIG`; secrets remain in local
`config.env` with `0600` permissions.
- Runtime proxy variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`,
`NO_PROXY`, their lowercase forms, and `NODE_USE_ENV_PROXY`) may be preserved
in `config.env` and applied after startup, so a user LaunchAgent can match the
operator shell network path without carrying token/user/cwd env directly.
- Start-with-system is implemented by a user-level LaunchAgent under
`~/Library/LaunchAgents`.
- A macOS menu bar app may control service start/stop/restart/status and open
config/logs, but it is not a settings editor in this release.
- macOS `.pkg` artifacts install the CLI binary and tray app; the operator still
runs setup in their user context.

## Non-Goals

- No Linux systemd or Windows service support in this slice.
- No Keychain migration for Telegram tokens.
- No GUI settings form or token entry window.
- No privileged system LaunchDaemon.

## Consequences

- macOS onboarding can be binary/package first instead of source-build first.
- Service lifecycle becomes testable through fake launchctl runners.
- Proxy-dependent deployments remain compatible with the `CTR_GO_CONFIG`-only
LaunchAgent contract.
- Existing environment-only and manual daemon workflows keep working.

## Hosted-task maintenance boundary (2026-10-26)

App Server child environments carry `CTR_GO_HOSTED_TASK=1`. CLI start, stop,
restart, install and uninstall refuse to mutate service lifecycle when invoked
inside that task environment, before issuing launchctl or writing configuration.
Status remains available. The parent's environment is not modified.

An external restart uses `launchctl kickstart -k` for an already registered job,
without bootout/bootstrap. If kickstart fails, report the failure and retain the
job registration. An unloaded service may still be started normally.

The runtime MCP initialization explains that raw bootout, kill or self-restarting
migration scripts are also unsafe, and that already-applied migrations should be
no-ops. This is an operational guard, not a security sandbox: arbitrary full-access
commands can bypass the CLI and terminate their own host. Hosted maintenance is
not transparently detached or drained; operators should finish active turns and
use an independent maintenance process.
