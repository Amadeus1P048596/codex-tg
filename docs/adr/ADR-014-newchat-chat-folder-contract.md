# ADR-014: `/newchat` Creates Real Codex UI Chat Folders

Status: accepted

Date: 2026-05-05

## Context

`/projects` treats threads under `Documents/Codex` as Codex UI `Chats`. Before this ADR, `/newchat` created an App Server thread without a cwd and only marked it locally as `Chats`, so the result did not match Codex UI Chat folders such as `Documents/Codex/2026-04-28/tool-call`.

## Decision

- `/newchat <prompt>` creates a real cwd under `CTR_GO_CODEX_CHATS_ROOT` before calling App Server `thread/start`.
- The default Chats root is `~/Documents/Codex`; operators may override it with `CTR_GO_CODEX_CHATS_ROOT`.
- New Chat cwd names are `<root>/<YYYY-MM-DD>/<title-or-prompt-slug>`, with numeric suffixes for collisions.
- `/newthread <prompt>` is the no-Chat-folder escape hatch: it calls `thread/start` without a Telegram-selected cwd, but App Server may still attach the daemon default cwd.
- Omitting the prompt from `/newchat` or `/newthread` starts a title-then-prompt flow. The first plain-text message is the explicit thread title; the second is the first turn prompt and may include Telegram `localImage` inputs. A photo sent while the title itself is pending is rejected with guidance instead of being routed to the existing binding. The pending state survives daemon restart, expires after 15 minutes, and can be cleared with `/cancel`.
- Explicit titles are written to App Server through `thread/name/set` and retained as user-owned Telegram titles so later runtime refreshes cannot replace them. Interactive `/newchat` folder slugs come from the explicit title.
- The existing one-line forms remain supported and clear any older pending create request for the same chat/topic.
- App Server protocol is unchanged: `thread/start` receives either the generated Chat cwd or no Telegram-selected cwd.

## Consequences

- `/projects -> Open Chats` shows `/newchat` threads as real Codex UI Chats.
- No arbitrary filesystem path is accepted from Telegram.
- Existing no-cwd or default-cwd threads remain supported through `/newthread` and older cached rows.
- Telegram users can start either flow from the command menu and provide a concise title separately from the full prompt.
