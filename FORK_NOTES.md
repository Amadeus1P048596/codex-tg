# Fork Notice and Modifications

This repository is a community fork of
[`mideco-tech/codex-tg`](https://github.com/mideco-tech/codex-tg). It is not an
official upstream release and does not imply endorsement by the upstream
maintainers.

## Upstream and Attribution

- Original project: [`mideco-tech/codex-tg`](https://github.com/mideco-tech/codex-tg)
- Fork publisher: [`Amadeus1P048596`](https://github.com/Amadeus1P048596)
- Fork baseline: upstream `v0.5.0`, commit `41fa09a`
- Fork release: `v0.5.0-amadeus.1`

The fork retains the upstream Git history, `LICENSE`, and `NOTICE`. It is
distributed under the Apache License 2.0. Git history identifies every changed
file, while this document and the release notes provide a prominent summary of
the modifications. Names and trademarks remain the property of their
respective owners.

## What This Fork Changes

- Replaces Telegram's stream of raw tool/output cards with one per-turn
  activity card, including a typing grace period, throttled edits, aggregated
  user-facing activity, and in-place terminal state.
- Adds one foreground Codex session per Telegram chat/topic, plus `/home`, a
  durable `/inbox`, title-based `/threads`, and direct background-session
  switching.
- Adds title-then-prompt session creation, `/current`, `/title`, confirmed
  `/archive`, paginated `/unarchive`, and `/cancel` for pending creation flows.
- Makes the Telegram live-session writer lifecycle explicit with guarded
  acquire/release controls and idle automatic release inside its private runtime.
- Adds Telegram photo input through App Server `localImage`, isolated spawned
  App Server homes through `CTR_GO_CODEX_HOME`, and Windows archive-path
  compatibility.
- Adapts Markdown pipe tables into mobile-readable Telegram records and keeps
  photo inputs alive for bounded asynchronous App Server ingestion before
  private temporary-file cleanup.
- Uses independent Desktop and Telegram sessions while allowing an explicitly
  linked capability layer and a separate user-approved durable-memory store.
- Adds Telegram-native Scheduled tasks with a private definition store, durable
  due-slot claims, isolated per-run threads, and observer delivery, without
  depending on or sharing task state with Codex Desktop.
- Adds ADRs, feature briefs, contract documentation, and regression tests for
  the new routing, lifecycle, navigation, rendering, and compatibility rules.

See [`CHANGELOG.md`](CHANGELOG.md) and
[`docs/release/v0.5.0-amadeus.1.md`](docs/release/v0.5.0-amadeus.1.md) for the
release-level summary.

## Validation

The publication candidate was checked on Windows with Go 1.26.7 using:

```powershell
gofmt -l <all Go files>
git diff --check
go test ./...
go build -buildvcs=false ./...
```

A targeted scan found no embedded Telegram bot token, GitHub token, OpenAI key,
private key, or private user path in the publication candidate. Existing
validation notes record partial real Telegram readback for the underlying local
changes; the full live Telegram matrix was not rerun solely for this fork
publication. Paths explicitly marked pending in
[`docs/testing/validation-notes.md`](docs/testing/validation-notes.md) should
therefore be treated as not fully live-validated.

This fork release publishes source and GitHub-generated source archives. It does
not claim that upstream binary packages contain these modifications.

## Thanks

Sincere thanks to the `mideco-tech/codex-tg` maintainers and contributors for
the original design, implementation, documentation, tests, and the permissive
Apache-2.0 foundation that made this fork possible.
