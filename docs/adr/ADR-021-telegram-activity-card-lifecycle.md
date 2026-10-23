# ADR-021: Telegram Activity Card Lifecycle

- Status: accepted
- Amends: ADR-003, ADR-004, ADR-008, ADR-010, ADR-013, ADR-015
- Related: `docs/process/telegram-output-style-v2-brief.md`,
  `docs/process/telegram-photo-output-bugfix.md`

## Context

The observer UI mapped Codex commentary, tool, output, run, and final events to
separate Telegram messages. Tool events can change several times per second, so
the chat repeatedly added, edited, and deleted cards. Internal fallback labels
such as `Last completed tool` and `No completed tool output yet.` were visible
as product copy. The result preserved raw observability but behaved like a live
debug console and made the actual task state hard to scan.

Telegram chat actions and in-place message editing provide a stable alternative.
Codex App Server already supplies enough normalized turn and item state to
derive user-facing activities without exposing each transport event.

## Decision

- A Codex turn owns one summary message whenever possible.
- In `per_run` mode, a changed App Server turn id always owns a new summary
  message, even when the previous panel still carries a stale non-terminal
  status. The four-second typing delay must not make the new turn inherit or
  edit an earlier turn's Telegram message.
- For the first four seconds, Telegram shows only `typing`. A surviving turn
  creates a Working card; a fast turn creates one terminal card.
- All non-terminal updates edit the summary message and are throttled to a
  four-second minimum interval.
- Input-required transitions bypass the throttle and edit the same card.
- A foreground terminal transition sends its result as a new normally notifying
  card, persists that message as the panel summary, then deletes the old Working
  card best-effort. A failed send preserves the Working card for retry.
- No additional compact `Completed` notice is sent; the new result card is the
  single terminal surface and notification.
- The leading emoji remains the stable conversation identity. State is conveyed
  with the words Working, Needs input, Completed, Failed, or Cancelled.
- The thread/task title is the primary first row. `Codex · State · duration` is
  the secondary row; only cards without an available title use the compact
  single-row fallback.
- An Activity Aggregator de-duplicates raw tool events, counts operations,
  prioritizes meaningful work, and retains up to three recent activities.
- Fast incidental tools are normally count-only. One compact current command may
  be shown when it helps technical users understand an important operation.
- Raw tool/output evidence stays available through explicit Details and export
  actions, not the default card.
- A running card renders only Stop, Steer, and Switch thread. A completed card
  renders only Details and Switch thread. The picker opens as a separate
  navigation response and does not replace the source activity card.
- Short task/run ids are bottom metadata. Full ids are opt-in diagnostics.
- Multi-paragraph final answers that fit the activity-card budget render the
  complete answer only in the expandable block. The opening paragraph is not
  repeated outside that block.
- Long final answers use the new terminal card for their compact summary and
  single notification, then may continue in silent rendered Result messages.
- Successful images produced by the terminal turn are sent after its card or
  compact background notice as silent Telegram photo companions. Durable
  per-target/thread/turn/image fingerprints prevent poll or restart duplicates;
  failed delivery stays retryable.
- Explicit final-answer Markdown images are attachments only when their resolved
  files remain inside the thread cwd. App Server-owned structured generated
  image paths are authoritative for generated-artifact locations outside it.
- Telegram photos are converted to App Server `localImage` inputs and enter the
  same single-card lifecycle; no media receipt card is added.
- Telegram photo albums are buffered by chat, topic, sender, and
  `media_group_id` until a one-second quiet window elapses. The caption from any
  member and every member's largest photo variant are then dispatched together
  as one App Server input, in Telegram message order.
- Each Telegram chat/topic has one foreground thread. Only that thread may show
  a live Working/Activity card. Other threads suppress progress cards and emit
  at most one compact terminal or needs-input notice with a switch action.
- Switching foreground threads removes the previous non-terminal card and
  renders the selected thread's current card, preserving one stable focus. If
  the previous foreground thread was already terminal, switching records that
  terminal fingerprint as acknowledged before observer polling can reinterpret
  it as a new background completion; genuinely running work still notifies when
  it later reaches a terminal state.
- `/threads` lists only threads returned by the current Telegram App Server
  runtime. Cached threads missing from that runtime are not selectable, thread
  titles are the inline buttons rather than numbered `Open` actions, and
  cursor-backed five-item pages expose older sessions.
- Interactive `/newchat` and `/newthread` flows collect an explicit title before
  the first prompt and write it through to App Server. After the plain-text title,
  that first prompt may contain Telegram `localImage` inputs. Legacy one-line
  forms use the first prompt as a display-title fallback while App Server still
  reports an id or generic placeholder. `/title` writes through to App Server
  and refreshes the current card in place.
- `/archive` requires an inline confirmation for the current foreground thread.
  `/unarchive` reads App Server's archived list with ten-row cursor pagination
  and uses each thread title as the restore action.
- `/start` and `/home` render one session hub. `/inbox` persists background
  terminal and needs-input attention items per Telegram chat/topic; selecting an
  item switches foreground and clears it.
- Manual `/title` values are user-owned and survive later runtime refreshes.
- Running or input-blocked sessions cannot be archived. Archive and unarchive
  success cards expose useful next actions instead of ending the navigation flow.
- Primary status labels and action buttons use consistent Chinese product copy;
  internal App Server state names remain unchanged.

## Consequences

- Telegram remains transparent about what Codex is doing while message position
  and hierarchy stay stable.
- The UI can lag raw event arrival by up to four seconds during active work.
  Input-required and terminal transitions are not delayed.
- Operation counts are execution telemetry, not a one-to-one list of visible
  rows.
- Existing Tool/Output rendering can remain behind explicit drill-down actions
  for diagnostics, but it is no longer part of the default turn chronology.
- A turn can own a small number of photo companion messages in addition to its
  stable activity card; those media do not reintroduce per-tool message spam.
- A failed typing request produces the Working card immediately so a Telegram
  API degradation cannot leave the user with no feedback.
- Session isolation means Desktop-only cached history is deliberately absent
  from `/threads`; shared skills, rules, plugins, and memory are unaffected.

## Non-goals

- Removing tool observability or full-log exports.
- Streaming every model token into Telegram.
- Rendering full UUIDs in normal cards.
- Using a photo as the pending title or attaching media to the one-line command
  forms in this slice.
