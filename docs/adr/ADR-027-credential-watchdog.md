# ADR-027: Opt-in access-token credential watchdog

Status: accepted. Amends ADR-019; complements ADR-018.

## Context

Copying a managed auth.json between independently refreshing clients can leave
one client holding an already-consumed refresh token. Process keepalive does not
repair this. The Windows validation note mentions a local credential watchdog,
but its source is absent from this repository and available history. This is a
new implementation of the recovery goal, not a port of that private script.

## Decision

An explicit `CTR_GO_AUTH_SOURCE_FILE` and `CTR_GO_AUTH_ACCOUNT_ID` opt both private
App Server clients into external-token mode. The source login owner remains the
only refresh-token owner. The existing launchd-supervised daemon hosts the
watchdog; no separate service or authentication App Server is added.

- Start with ephemeral credential storage; never load old private auth.json or
  fall back to inherited OpenAI/Codex API keys. Require a separate runtime home.
- After initialization, load only the source access token and pinned account via
  `account/login/start` with `type=chatgptAuthTokens`.
- Read the source every 30 seconds, before a new turn, and on
  `account/chatgptAuthTokens/refresh`. Import changed tokens without restarting
  processes. Answer refresh requests internally with their original RPC ID,
  guarded by process generation. Do not route them as approval requests.
- Never copy/write source credentials or refresh tokens. Never change Desktop
  sessions, SQLite, caches or locks. Source files must be bounded regular files.
- Locally screen JWT expiry (30-second margin) and account against an explicit
  pin. JWT decoding is not signature validation; the server validates actual
  authorization on use. Health says "access token loaded", not "authenticated".
- Missing, malformed, expired, switched-account, and unchanged rejected tokens
  fail closed for new turns. Report sanitized status, keep Telegram available,
  and recover when a fresh matching source token appears. No restart loop or
  automatic replay of failed turns. Existing unrelated tasks are not interrupted.
- Missing sources at startup do not prevent the transport from coming online;
  the pre-turn guard prevents inference without valid external credentials.

## Consequences

This eliminates refresh-token competition in opt-in mode, but cannot guarantee
unattended renewal if the source owner stops refreshing or its login is revoked.
The owner must sign in again. File-backed auth sources are supported; OS keychain
sources and a new interactive login ceremony are outside this slice. External
App Server token mode is experimental; incompatible CLIs report login failure.
The feature is off by default and requires an updated daemon plus explicit
configuration. Existing managed authentication remains unchanged when disabled.

## References

- https://learn.chatgpt.com/docs/app-server (external ChatGPT tokens and refresh)
- https://learn.chatgpt.com/docs/auth (credential storage and managed refresh)
