# Windows Codex Runtime Failover Brief

## Problem

Codex Desktop installs CLI bundles below content-addressed directories. A
Desktop update may remove the exact directory configured in
`CTR_GO_CODEX_BIN`, leaving `codex.exe` or a required sibling host unavailable
to the Telegram daemon.

## Goal

Keep Telegram available across Desktop updates by running App Server children
from a codex-tg-owned, complete runtime copy with an active/previous rollback
boundary.

## Non-goals

- Keeping Desktop and Telegram on exactly the same Codex build.
- Sharing Desktop sessions, databases, caches, or App Server processes.
- Restarting a healthy daemon merely because a newer bundle appears.

## UX / Operator Flow

On Windows, daemon startup resolves a private active runtime. If none exists,
it copies and probes the newest complete local Desktop bundle. While running,
it periodically stages a newer complete bundle for the next restart. A newly
promoted bundle must connect both live and poll App Server sessions before it is
confirmed; otherwise startup swaps back to the previous confirmed bundle.

## Domain Model

- managed runtime: private immutable version directories under codex-tg home;
- active version: selected for the next daemon start;
- previous version: rollback target;
- confirmed version: connected both App Server sessions successfully;
- complete bundle: `codex.exe` plus all required Windows sibling executables.

## Architecture

`internal/codexruntime` owns discovery, completeness checks, copying, probing,
state persistence, promotion, confirmation, and rollback. `cmd/ctr-go` owns the
daemon-start validation loop and the non-disruptive background sync schedule.

## Testing

- Unit tests cover bootstrap, source deletion, incomplete candidates, promotion,
  confirmation, and rollback.
- Full Go test/build gates remain required.
- Live rollout checks both App Server connections and the resolved binary path.

## Acceptance Criteria

- [x] Removing the configured Desktop bundle does not break a previously bootstrapped runtime.
- [x] Incomplete or non-executable candidates never replace the active version.
- [x] A new version is staged without interrupting the running daemon.
- [x] Failed first startup of a promoted version rolls back to the previous version.
- [x] All Windows child processes used here are created without a visible console window.
