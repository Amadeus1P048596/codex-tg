# ADR-015: Telegram Notification Contract

- Status: accepted
- Supersedes: the terminal finalization message-edit detail in ADR-004 and ADR-010.
- Amended by: ADR-021 stable activity-card lifecycle and its 2026-10-21
  notifying-final replacement.

## Context

Telegram notification volume can become noisy when every observer card is sent as a normal message. The operator still needs audible attention for genuinely important events: a new run, a final answer, and a Plan Mode question that needs a choice or reply.

Telegram edits do not provide a reliable per-edit notification contract. Therefore a `[Final]` that is produced by editing the live `[commentary]` card cannot be made audible without also making the earlier commentary message audible.

## Decision

- New messages are silent by default through Telegram Bot API `disable_notification=true`.
- Audible messages are limited to:
  - `New run`, controlled by `CTR_GO_NOTIFY_NEW_RUN` and enabled by default;
  - a newly sent terminal card;
  - a routeable `[Plan]` prompt-card for user input or structured choices.
- `[commentary]`, `[Tool]`, `[Output]`, `[User]`, command/menu responses, explicit exports, and fallback/error messages are silent.
- Finalization sends the complete terminal card as a new audible message because
  Telegram cannot notify on an edit. After that send is persisted successfully,
  the old silent Working card is deleted best-effort.
- If the terminal-card send fails, the Working card and retryable panel state are
  preserved. A fast turn with no Working card sends the same audible terminal
  card without a deletion step.
- A long result uses the terminal card as its single completion notification;
  following `Codex · Result` messages are silent companions.
- Details and Back callbacks are rebound to the new terminal card message id.

## Consequences

- The operator receives fewer notifications while preserving alerts for run start, required Plan input, and run completion.
- The chat retains one primary card after best-effort cleanup while completion
  produces a real Telegram notification.
- The terminal card, rather than a separate one-line completion notice, is the
  de-duplicated attention and interaction surface.

## Non-goals

- This does not add per-chat notification profiles.
- This does not make Telegram edits audible.
- This does not change App Server protocol or Plan Mode routing.
