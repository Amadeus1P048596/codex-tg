# macOS credential watchdog

The main LaunchAgent already keeps the daemon running. This optional watchdog
runs inside that daemon and supplies fresh access tokens to its two isolated App
Servers. It does not need another LaunchAgent, a timer automation, or permission
to restart your tasks.

The Windows validation note mentions a local credential watchdog, but that
script was not checked in. This implementation follows the documented recovery
goal; it is not a verified source port of the Windows implementation.

## Setup

First install/build a version containing ADR-027. Choose an existing file-backed
ChatGPT login owner (for example Codex Desktop's auth.json). Its application must
continue managing its own login and token renewal. Keep `CTR_GO_CODEX_HOME`
pointing at a separate, absolute Telegram runtime directory.

Configure using the bundled helper (substitute your actual config/source paths):

```sh
python3 scripts/configure-macos-auth-watchdog.py \
  --config "$HOME/.codex-tg/config.env" \
  --source "$HOME/.codex/auth.json"
```

The helper atomically writes only the source path and pinned account identifier
into your existing private config, with mode 0600. It preserves other settings
and never copies access/refresh tokens. Do not commit this config. It neither
installs the candidate binary nor restarts a live service.

The resulting keys are `CTR_GO_AUTH_SOURCE_FILE` and `CTR_GO_AUTH_ACCOUNT_ID`.
The source must remain readable by the service user. A keychain-only login is not
supported by this file-source adapter. Do not export credentials into launchd
plist environment variables.

When active turns finish, replace/install the daemon binary and restart from an
independent terminal using `ctr-go service restart` for a standard installation.
For a custom LaunchAgent, use its existing deployment procedure and label. A
binary replacement may require rechecking macOS Full Disk Access, especially
with ad-hoc signing. Do not restart the hosting daemon from a Telegram task.

## Behavior and diagnosis

- Every 30 seconds and before each new turn, read current source credentials.
  Matching, unexpired changed access tokens are loaded without a process restart.
- On an App Server authentication-refresh request, reread the source and respond
  directly. A rejected unchanged token produces an explicit error instead of
  repeatedly reusing a refresh token.
- Telegram `/status` displays `Credential watchdog:`. "Access token loaded"
  means local validation and protocol login succeeded, not a network health
  check. OpenAI validates authorization when the token is used.
- Missing/expired credentials require opening or signing in to the source owner.
  Once a fresh token appears, recovery is automatic; retry the failed message.
  Failed tasks are never replayed automatically.
- An account change is refused. Re-run configuration only if you intentionally
  want to change which account funds new work, then restart when idle.
- There is no separate proactive refresh of the source. If Desktop stays closed
  long enough for its token to expire, this cannot keep it logged in forever.

To disable, remove both watchdog keys from the private config and restart when
idle. The old private auth.json is never modified by this feature; standard
managed mode will require its own valid login. Do not restore an old copied
refresh token as a repair strategy.

## Validation

```sh
go test -race ./internal/credentials ./internal/appserver ./internal/config \
  -run 'TestSource|TestCredential'
python3 scripts/test_macos_auth_watchdog.py
```

An opt-in `TestCredentialWatchdogLive` initializes a temporary isolated home,
loads external access credentials and calls `account/rateLimits/read`, checking
that no auth.json is persisted. Set `CODEX_TG_AUTH_LIVE_TEST=1`,
`CODEX_TG_AUTH_TEST_SOURCE` and `CODEX_TG_AUTH_TEST_BIN` explicitly. It sends no
Telegram message and starts no model turn. Do not log source credential contents.
