# macOS credential watchdog

Provide opt-in recovery from rotated desktop credentials without copying refresh
tokens or sharing runtime state. Run inside the existing launchd-supervised
codex-tg daemon; no second App Server broker or restart loop.

The Windows validation note describes a local watchdog but its implementation is
not in this repository or its available Git history. This is a new implementation
of that operational goal, not a source port or a claim of Windows parity.

Use an explicit auth.json source and pinned account. Initialize both private App
Servers with ephemeral credential storage and external access-token login. Poll
for source changes, check again before turns, and handle external-token refresh
requests. Never refresh the source credential, mutate its file, or fall back to
previous private refresh tokens. Reject account changes and expired tokens.
Expose sanitized health in /status. Missing/revoked source authorization still
requires the owner to sign in; do not silently replay failed user tasks.

Tests cover token validation, recovery after rotation, missing/expired source,
account switching, unchanged rejected tokens, server-request errors, generation
isolation, ephemeral startup, and configuration wiring. Live service deployment
and Telegram messages are separate from source publication.
