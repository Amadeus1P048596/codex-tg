# ADR-024: Windows Codex Runtime Failover

- Status: accepted
- Related: ADR-017, ADR-019

## Context

Codex Desktop distributes the CLI as a bundle in a content-addressed directory.
The main executable depends on sibling Windows executables. Pinning
`CTR_GO_CODEX_BIN` directly to that directory couples Telegram availability to
Desktop update cleanup, even though the two clients intentionally have separate
App Server lifecycles and mutable runtime state.

## Decision

- Managed runtime mode is enabled by default on Windows and remains opt-out.
- codex-tg copies complete CLI bundles into its private home and starts both App
  Server children from the private active version.
- Bundle completeness includes the main CLI, code-mode host, command runner,
  and sandbox setup executable.
- New local Desktop bundles are discovered and staged in the background. A
  promotion changes only the next daemon start and never kills the running App
  Server processes.
- Structural checks and `codex --version` are preflight gates. The first daemon
  start then validates both App Server sessions before confirming the version.
- The last active version is retained as `previous`. Failure to validate a new
  active version triggers one rollback attempt.
- Binary isolation does not weaken ADR-019: Desktop and Telegram still do not
  share sessions, databases, writer locks, caches, or App Server processes.
- Windows probes and App Server children must start without visible console
  windows and write diagnostics through existing logs.

## Consequences

- Desktop may be newer than Telegram between background sync and the next safe
  restart; exact version parity is not an availability requirement.
- The private runtime uses additional disk space for active and previous
  versions.
- Operators can disable management with `CTR_GO_CODEX_RUNTIME_MANAGED=false`
  for custom installations.
- A valid private active version remains usable after Desktop removes the
  originally configured content-addressed directory.
