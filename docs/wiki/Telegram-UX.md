# Telegram UX

## Run Chronology

Each Codex turn normally owns one Telegram activity card. For the first four
seconds the bot sends only `typing`; if the turn is still active, it creates:

```text
❤️ Codex-TG Activity Card
Codex · 处理中
正在处理请求 · 38s

进度
✓ 搜索 output 相关实现
✓ 查看 internal/daemon/output.go
● ✏️ 修改输出裁剪逻辑

9 次操作 · T:01a03706
```

The leading emoji is a stable conversation identity, not a status signal. The
thread/task title is the primary first row; `Codex · State · duration` is the
secondary status row. If App Server has not supplied a title yet, the renderer
temporarily falls back to the compact single-row status header. The primary UI
uses the Chinese states `处理中`, `需要输入`, `已完成`, `失败`, and `已取消`. Tool,
output, and lifecycle events are de-duplicated and aggregated into at most three
activities. Non-terminal edits are limited to one every four seconds. Fast
reads, searches, and status checks usually affect only the operation count.

Raw `[Tool]`, `[Output]`, `Last completed tool`, and empty-output messages are
not part of the normal chronology. Details and export actions retain the raw
evidence for diagnostics.

## Final Card

The Working card is edited in place to its terminal state. It is not deleted and
replaced with a new Final message. Fast turns that finish before the four-second
grace period produce only one terminal card.

Short final answers appear directly on that card and add one compact audible
completion notice because Telegram does not notify for message edits. Long answers first complete
the original card with a compact summary, then continue in one or more
`Codex · 结果` messages. Completed commentary and tool/output history remain
available through Details, Tools file, and full-log actions. Details/Back stay
bound to the completed card and turn that created them.

## Notifications

Typing, Working creation, repeated activity edits, exports, and direct menu
responses are silent. A short foreground terminal edit sends one de-duplicated
compact audible notice; a fast terminal card or separately sent long result is
already audible and does not add that notice. Needs-input retains its product
notification policy. Tool event bursts never create individual notifications.

## Session Home, Inbox, And Thread Picker

`/start` and `/home` open the same compact session home. It shows the foreground
session title, current state, and elapsed time. A separate `后台运行` section
lists the title, `处理中` state, and elapsed time of every other running session;
the five most recent are expanded as direct view buttons and any remainder is
counted. The section uses already reconciled SQLite thread/snapshot state so Home
stays responsive and does not contend with active writers. Home also shows the
durable count of background sessions needing attention. Its other buttons open
the current card, runtime session picker, Chat/ordinary-session creation chooser,
or `/inbox` in place.

`/inbox` is SQLite-backed and survives daemon restarts. It retains one current
Completed, Failed, Cancelled, or Needs-input item per background session. Items
are title buttons; switching to one clears it from the inbox.

Each Telegram chat/topic has one foreground Codex session. Its Working card is
the only live progress card shown. Progress-card edits from other sessions are
suppressed, while their compact running states remain visible on demand through
Home. This keeps passive navigation observable without letting several cards
refresh continuously. When a background session completes, fails, is cancelled,
or needs input, Telegram sends one compact notice with a `切换至该会话` button.

Switching removes the previous foreground Working card, makes the selected
session the foreground and bound session, and shows its current card. This keeps
concurrent work observable without making several progress cards jump around.

`/threads` is an authoritative picker for the current Telegram App Server
runtime. It shows one clickable title button per available session and no
numbered `Open 1`, `Open 2`, or cached debug description rows. A placeholder
App Server title falls back to the session's latest prompt preview. Cached
Desktop sessions that are absent from the isolated Telegram runtime are not
shown and cannot be opened as stale Completed cards.

`/current` shows the foreground session marker, title, Chinese status, and short
task id. `/title <new title>` renames that current App Server thread, records the
title as user-owned, and edits its existing Activity Card in place. Runtime
refreshes cannot overwrite a user-owned title. Interactive `/newchat` and
`/newthread` flows collect and write through an explicit title before asking for
the first prompt. Legacy one-line creation uses its prompt as a temporary title
whenever App Server still returns a UUID or generic placeholder; a later real
App Server title replaces only that automatic fallback. Creating or binding a
session also makes it the foreground session.

`/archive` targets only the current foreground session. Running and input-blocked
sessions are rejected before confirmation. An idle session shows inline
`确认归档` / `取消`; success lands on `切换其他会话` and `新建会话` instead of a
dead end. `/cancel` remains the immediate
cancel action for pending `/newchat` and `/newthread` creation and has no archive
confirmation behavior. `/unarchive` lists the current Telegram runtime's real
archived threads ten per page; each title is a restore button, with inline
previous/next navigation. Restore success offers both `切换至该会话` and
`继续查看归档`.

On affected Windows App Server versions, a resumed idle session is archived
through a fresh live generation to avoid a cached extended-path failure. The
daemon first confirms that every writer owned by the old generation is idle,
archives the target through App Server, and restores the remaining writers. If
another owned session is active or cannot be verified, archive fails closed and
does not interrupt it.

## Photo Input

A pure Telegram photo or photo with a caption is accepted on a routed or
reply-targeted thread and as the first prompt after an interactive `/newchat` or
`/newthread` title. The bot chooses the largest Telegram photo variant,
downloads up to 20 MiB into a private temporary directory, and starts or steers
the turn with the caption plus an App Server `localImage` input. With no caption,
it supplies a short default image-analysis prompt. A photo sent while the title
itself is still pending is not routed to the existing binding; the bot asks for
a plain-text title first. The private temporary input remains available for 30
minutes after dispatch because App Server may read a `localImage` asynchronously
after accepting the RPC. Scheduled cleanup removes it afterward; startup and
later downloads also remove matching files older than 24 hours. No separate
media-receipt card is created.

Media groups, image titles, and media attached to one-line `/newchat <prompt>`
or `/newthread <prompt>` command forms are not covered by this implementation
slice.

## Markdown Tables

Telegram has no native table entity and proportional mobile fonts make raw pipe
tables hard to read. Outside fenced code blocks, the renderer rewrites a
GitHub-style Markdown pipe table into one labeled list record per source row.
The first column becomes the bold record title; remaining column names become
bold field labels stacked below it. Inline code and other supported Markdown
inside cells continue through the normal Telegram entity converter. Pipe-table
examples inside fenced code blocks remain unchanged.

## Plan Mode

Telegram can start a Codex Plan Mode turn with `/plan <thread> <text>`, `/plan_mode <thread> <text>`, or `/reply --plan <thread> <text>`. These commands use App Server `turn/start` with `collaborationMode.mode = plan`; prompt wording alone is not treated as Plan Mode.

If a thread remains in Plan Mode after a completed turn, the Plan Final Card shows `退出 Plan`. Pressing it sets a one-shot local reset for that thread; the next ordinary `turn/start` is sent with `collaborationMode.mode = default` and then the reset is cleared. `/stop <thread>` sets the same one-shot reset even when the thread is already idle.

When Codex asks for input, the bridge renders a separate routeable `[Plan]` prompt-card. Replying to that card answers the same run. Structured buttons appear only when Codex provides choices.

Plan answer buttons are scoped to their own turn. A pending Plan prompt from an
older turn must not appear under a newer `[commentary]` card for the same
thread.

## New Threads

`/projects` opens a project/workspace menu from cached Codex thread metadata.
The `New thread` action arms the current chat/topic so the next plain-text
message creates a new App Server thread in that project cwd and starts the first
turn with that text.

Cached threads whose cwd is under `Documents/Codex` or the configured
`CTR_GO_CODEX_CHATS_ROOT` are treated as Codex UI `Chats`, not normal projects.
The main `/projects` view shows recent project workspaces and the latest Chat
previews; `Open Chats` opens the full paginated Chat list. Selecting a Chat
opens and binds that single thread.

New Chat starts can use `/newchat` followed by a title and then a separate first
prompt. The bot keeps the pending stage and title for the current chat/topic for
15 minutes and offers `/cancel`. The bridge creates a dated Chat folder from the
title under the configured Chats root, passes that cwd to App Server
`thread/start`, writes the title through, and starts the first turn using only
the prompt. `/newthread` uses the same title-then-prompt interaction without
project selection or Chat folder creation. The one-line `/newchat <prompt>` and
`/newthread <prompt>` forms remain supported. App Server may still report the
daemon default cwd for a `/newthread` thread.

The Telegram bot does not accept arbitrary filesystem paths for this flow.
Creating or editing project work directories is a separate future feature.

## Codex Settings

`/settings`, `/model`, and `/effort` expose Telegram button menus for model selection and reasoning effort used by Telegram-started collaboration-mode turns. The selections are stored in SQLite daemon state and are not configured through public env vars.

After a model or reasoning-effort selection, the menu message is edited into a compact settings summary without inline choice buttons. Use `/settings`, `/model`, or `/effort` to reopen the menus.

## Independent Sessions, Shared Capabilities And Memory

Telegram does not share the Windows Codex Desktop App Server or mutable runtime.
Its dedicated `CTR_GO_CODEX_HOME` has separate sessions, state databases, writer
locks, and caches. `/threads` therefore lists only Telegram-runtime history.

Static capabilities such as Skills, plugins, packages, and global instructions
may be shared through explicit filesystem links. Cross-client durable memory uses
a separate application-level store and contains only user-approved stable facts,
preferences, and conventions; it does not share conversation history, thread ids,
runtime state, or secrets.

Inside the Telegram runtime, `/show` and observer-only tracking use the read-only
poll App Server. `/bind` and `在 TG 中继续` load the thread in the live App Server.
An unexpected writer conflict from another connection in that isolated runtime is
reported without queuing the user's message or creating a parallel turn.

While the live App Server owns the writer, summary and Final cards show
`释放空闲写入权` instead of `在 TG 中继续`. After a Telegram-started turn is idle,
use that button or `/release` to recycle the live session. Release applies to all
idle threads owned by that live generation and persists a marker so background
refresh or restart does not immediately reacquire them. It refuses to run if any
owned thread is still active, waiting for approval/input, or cannot be verified.
The daemon invokes the same guarded, session-wide release automatically after
five minutes without an allowed Telegram message or button action. New Telegram
activity resets the timer; an active or pending thread delays release until a
later safety check succeeds.

## Exports

- `Tools file`: on-demand file for selected Details tool/output.
- `Get full log`: on-demand archive from Codex session JSONL.

Automatic tool-output document spam is intentionally forbidden.
