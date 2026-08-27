# ADR-015: Telegram Notification Contract

- Status: accepted
- Supersedes: the terminal finalization message-edit detail in ADR-004 and ADR-010.
- Amended by: ADR-021 stable activity-card lifecycle.

## Context

Telegram notification volume can become noisy when every observer card is sent as a normal message. The operator still needs audible attention for genuinely important events: a new run, a final answer, and a Plan Mode question that needs a choice or reply.

Telegram edits do not provide a reliable per-edit notification contract. Therefore a `[Final]` that is produced by editing the live `[commentary]` card cannot be made audible without also making the earlier commentary message audible.

## Decision

- New messages are silent by default through Telegram Bot API `disable_notification=true`.
- Audible messages are limited to:
  - `New run`, controlled by `CTR_GO_NOTIFY_NEW_RUN` and enabled by default;
  - a newly sent terminal card;
  - one compact terminal notice when a foreground Working card is completed in place;
  - a separately sent long final result, which replaces the compact terminal notice for that turn;
  - a routeable `[Plan]` prompt-card for user input or structured choices.
- `[commentary]`, `[Tool]`, `[Output]`, `[User]`, command/menu responses, explicit exports, and fallback/error messages are silent.
- Finalization edits an existing Working card in place. Because Telegram edits do not notify, a short foreground result also sends one de-duplicated compact terminal notice as a new audible message.
- A fast turn with no Working card sends one audible terminal card. A long result edits the card and sends the full result as the audible new message, without an additional compact notice.
- Details and Back callbacks remain bound to the completed run panel/card and its stable summary message id.

## Consequences

- The operator receives fewer notifications while preserving alerts for run start, required Plan input, and run completion.
- The completed-run surface remains the same stable Final/Details message that previously showed Working state.
- Compact terminal notices are attention signals, not replacement activity cards, and are de-duplicated per target/thread/turn/final fingerprint.

## Non-goals

- This does not add per-chat notification profiles.
- This does not make Telegram edits audible.
- This does not change App Server protocol or Plan Mode routing.
