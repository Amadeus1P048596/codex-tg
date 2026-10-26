# ADR-025: Default Full Access For The Telegram Runtime

- Status: accepted
- Scope: this community fork's Telegram-private runtime

## Decision

`CTR_GO_FULL_ACCESS` defaults to `true`. The daemon starts its App Servers with
`approval_policy=never` and `sandbox_mode=danger-full-access`. New, resumed and
forked threads also receive explicit `approvalPolicy=never` and
`sandbox=danger-full-access` RPC parameters, since saved permissions on an old
thread can otherwise override process defaults. Scheduled runs use the same
thread creation path.

Setup commands write this default explicitly and keep slash-command menus on.
The service installer preserves an existing explicit opt-out and menu setting.

Existing active and archived threads are updated when resumed; this does not
require unarchiving all history at startup or editing the App Server database.
No private credentials, runtime configuration, session history or state database
are distributed with the repository.

## Operator Control

Set `CTR_GO_FULL_ACCESS=false` to stop forcing full access and use the configured
Codex defaults and saved thread permissions. This switch does not automatically
downgrade already-persisted permissions. Keep Telegram user/chat allowlists
restricted: full access also removes local approval requirements for scheduled
work.

Permission approvals that still occur through an explicitly configured policy
use the independent, routeable notification described in ADR-015.

## Validation

- Configuration defaults on and accepts explicit opt-out.
- New and resumed threads get overrides only when full access is enabled.
- Setup persists explicit defaults, and reinstall preserves an opt-out.
