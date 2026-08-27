# Telegram Output Style v2 — Activity Card Brief

Status: implemented; automated validation required before deployment.

## Goal

Keep Codex execution observable without turning Telegram into a debug console.
One Codex turn should normally own one stable message whose lifecycle moves from
Working to a terminal state in place.

## Message Lifecycle

- During the first four seconds, send only Telegram `typing` chat actions.
- If the turn is still active after the grace period, create one Working card.
- Fold tool, output, plan, and lifecycle changes into that card with
  `editMessageText`.
- Throttle non-terminal edits to one update per four seconds.
- Complete, fail, cancel, or request input by editing the same message. Do not
  delete the Working card and send a replacement.
- If a turn finishes before a Working card exists, send one terminal card.
- If a final answer is longer than the compact-card budget, complete the card
  with a summary and send the full result in a following `Codex · Result`
  message.
- Telegram API failures for `typing` fall back immediately to a Working card.

## Visual Contract

The leading emoji is the conversation marker. It identifies the session and is
not replaced by a status icon. Status is written as text.

```text
❤️ codex-tg activity card
Codex · Working
正在处理请求 · 1m 06s

Activity
✓ 修改输出裁剪逻辑
✓ 格式化代码
● 🧪 运行测试
go test ./internal/daemon/...

16 operations · T:01a03706
```

Terminal form:

```text
❤️ codex-tg isolation
Codex · Completed · 1m 21s
已重构 daemon 输出裁剪逻辑，并通过相关测试。

Changes
• 合并重复逻辑
• 调整 Telegram 输出截断
• 补充边界情况处理

18 operations · T:01a03706 · R:01a037d2
```

Fixed status vocabulary:

- `Working`
- `Needs input`
- `Completed`
- `Failed`
- `Cancelled`

Task and run ids are shortened in the default card and rendered as low-weight
code metadata at the bottom. Full ids remain available through the existing
diagnostic action.

The primary first row is the App Server thread/task title prefixed by the stable
conversation marker. The Codex role, textual state, and terminal duration live
on the second row. A missing title falls back to the compact single-row status
header until a later snapshot supplies one.

## Activity Aggregator

Raw Codex events are inputs to the aggregator, not Telegram messages. The
aggregator:

- merges bursts of events;
- de-duplicates repeated tool ids and labels;
- counts operations;
- selects the most meaningful current activity;
- retains at most three recent activities;
- keeps at most one compact real command for technical visibility.

Fast reads, searches, and status checks normally contribute only to the
operation count. They become visible only when no more meaningful activity is
available. Long-running or important edit, format, test, build, and inspection
operations take priority.

Default mappings:

| Raw operation | Telegram activity |
| --- | --- |
| `rg`, `grep`, search | `🔍 搜索代码` |
| `read`, `cat`, `Get-Content` | `📄 查看 {file}` |
| file change, `apply_patch` | `✏️ 修改 {file}` |
| `git diff`, `git status` | `🔍 检查变更` |
| `go test`, `npm test`, test runners | `🧪 运行测试` |
| formatter | `✨ 格式化代码` |
| build command | `🔨 构建项目` |
| unknown command | `⚙️ 执行命令` |

The default UI must never expose implementation labels such as `[Tool]`,
`[Output]`, `Last completed tool`, `No completed tool output yet.`, or
`Status: completed`. Raw evidence remains available through explicit Details,
Tools file, full-log, or a future verbose/debug mode.

## Rendering And Transport

- Renderer-owned messages use explicit Telegram `HTML` parse mode.
- Dynamic text is escaped before trusted tags are added.
- Each rendered message carries a paired plain-text fallback.
- Link previews are disabled by default.
- Inline keyboard buttons remain separate from formatted text.
- Content is split below Telegram's message limits without breaking entities.
- The compact visible body stays short; detailed evidence belongs in expandable
  blockquotes or explicit drill-down actions.

## Photo Input

- A pure photo or a photo with caption is valid turn input; it is not ignored.
- Select the largest Telegram photo variant, resolve it through `getFile`, and
  download it to a private temporary input directory.
- Submit the caption plus an App Server `localImage` user input. If no caption
  is present, use a short default image-analysis prompt.
- Limit downloads to 20 MiB. After App Server accepts the turn request, retain
  the private temporary file for 30 minutes so asynchronous `localImage` reads
  remain valid, then remove it. Startup and later downloads remove matching
  files older than 24 hours.
- Do not create a separate `Photo received` or User card. The turn follows the
  same typing-to-Working lifecycle.
- This slice supports one Telegram photo on an already routed or reply-targeted
  thread. Media groups and photo-first `/new*` flows are follow-up work.

## Notification Policy

- `typing` and Working edits are silent.
- Terminal transitions still edit the same activity card. Short foreground
  results also emit one de-duplicated compact audible notice because Telegram
  edits cannot notify; fast terminal cards and separately sent long results are
  already audible and do not add that notice.
- `Needs input` retains its product notification policy.
- Repeated tool events never create notifications.

## Acceptance Gates

- Aggregator tests cover classification, de-duplication, priority, recent-item
  retention, operation counting, and command compaction.
- Lifecycle tests cover four-second typing grace, typing failure fallback,
  four-second edit throttling, terminal in-place edit, de-duplicated audible
  completion notice, and long-final overflow.
- Renderer tests cover HTML safety, Chinese, emoji/UTF-16, Windows paths,
  Markdown, code, pipe-table-to-labeled-record conversion, and plain-text
  fallback.
- Photo tests cover Telegram file lookup/download, largest-size selection,
  caption/default prompt, App Server `localImage` serialization, bounded
  post-dispatch retention, and stale cleanup.
- Full `go test ./...`, `go build -buildvcs=false ./...`, and
  `git diff --check` pass before shadow deployment or cutover.
- Shadow validation must not start a second `getUpdates` poller with the live bot
  token. The existing daemon remains available until the replacement is built,
  checked, and ready for a controlled rollback-capable switch.
