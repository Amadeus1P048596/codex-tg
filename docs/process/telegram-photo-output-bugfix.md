# Telegram Photo Output Bugfix

## Goal

Deliver images produced by a completed Codex turn to the same Telegram
chat/topic as real Telegram photos, without exposing unrelated local files or
duplicating media during observer re-polls.

## Cause

The bridge implemented Telegram-to-Codex `localImage` input and text/document
output, but its sender interface and App Server snapshot normalizer had no
Codex-to-Telegram photo path. App Server `imageGeneration` items, dynamic-tool
`inputImage` content, and explicit local Markdown image references were reduced
to text or ignored.

## Behavior

- Normalize completed App Server `imageGeneration` items and completed dynamic
  tool `inputImage` content from the latest turn.
- Upload up to four PNG/JPEG payloads per turn, each within Telegram's 10 MiB
  `sendPhoto` limit, after the
  terminal card/notice, silently so companion images do not create notification
  bursts.
- Retry failed delivery and persist successful delivery fingerprints in SQLite
  so polling, restart, foreground changes, and terminal replay do not duplicate
  a photo.
- Accept explicit Markdown image paths only when the resolved regular file stays
  inside the thread working directory. App Server-owned structured
  `imageGeneration.savedPath` remains the authority for generated files outside
  that directory.
- Do not forward remote Markdown URLs, failed/in-progress generation items,
  unsupported media, oversized data, or generation prompts used internally by
  the image tool.
- Replace final-answer Markdown image markup with its human label before text
  rendering so Telegram cards do not expose local filesystem paths. Fenced-code
  examples remain literal and never become attachments.

## Non-goals

- Downloading arbitrary remote image URLs.
- Telegram media groups, GIF animation transport, or automatic document
  fallback for unsupported image types.
- Treating every ordinary local-file link in a final answer as an attachment.

## Acceptance

- App Server normalization tests cover structured image generation and dynamic
  tool image content while excluding failed generation.
- Telegram transport tests verify multipart `sendPhoto`, chat/topic routing,
  captions, media type, and silent delivery.
- Observer tests prove terminal delivery, base64 and saved-path handling,
  path-boundary enforcement, structured/Markdown de-duplication, and repeated
  poll de-duplication.
- Full Go tests/build/vet and public private-data scans pass. Real Telegram
  readback remains required after an operator-authorized daemon restart.
