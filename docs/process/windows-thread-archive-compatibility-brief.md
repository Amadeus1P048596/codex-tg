# Windows Thread Archive Compatibility Brief

## Problem

On Windows, Codex App Server 0.148.0 can persist an active thread's
`rollout_path` in `state_5.sqlite` with the extended-path prefix `\\?\` and
then reject `thread/archive` with `os error 2`, even though the rollout exists.

## Goal

Make Telegram `/archive` reliable for affected threads while keeping Codex App
Server's `thread/archive` method authoritative for the archive operation.

## Non-goals

- Reimplement thread archiving in codex-tg.
- Change non-Windows behavior.
- Rewrite session content or archive unrelated threads.

## Architecture

Immediately before `thread/archive`, the App Server client normalizes only the
target row's Windows extended `rollout_path` in the configured Codex home. It
then calls the existing App Server method unchanged. Missing databases, rows,
and ordinary paths remain no-ops; a prefixed path whose normalized source does
not exist fails closed.

## Testing

- Unit-test prefixed, ordinary, non-Windows, missing-file, and missing-state
  cases.
- Run `go test ./...` and `go build -buildvcs=false ./...`.
- Run the build-tagged live App Server test against an isolated Codex home
  before the normal idle-gated daemon cutover.

## Acceptance Criteria

- [x] A prefixed Windows rollout path is normalized before archive RPC.
- [x] The same `thread/archive` RPC remains the only archive operation.
- [x] Other platforms and ordinary paths are unchanged.
- [ ] The deployed daemon passes a live `/archive` regression check.
