# Windows Resumed Thread Archive Bugfix

## Goal

Archive an idle Telegram-runtime thread after it has been resumed by Codex App
Server 0.148.0 on Windows, while keeping App Server authoritative for the file
move and archive state transition.

## Cause

`thread/resume` rewrites the persisted rollout path with the Windows extended
drive-path prefix and retains that path in the loaded-thread cache. A later
`thread/archive` in the same App Server generation fails with `os error 2`, even
though the rollout file and archive directory exist.

## Behavior

- Normalize affected persisted paths before each App Server generation starts.
- Track resumed thread ids in generation-local memory.
- For a resumed target, verify every writer held by the live generation is idle.
- Replace the live generation, archive the target before any resume, then restore
  non-target writers.
- If any writer is active or cannot be verified, leave the generation untouched
  and return a retryable Telegram response.

## Non-goals

- Moving rollout files or updating archive state outside App Server.
- Interrupting an active turn to make archive succeed.
- Sharing Telegram and Desktop runtime databases or caches.

## Acceptance

- Unit coverage proves the generation-local cache, safe recycle, writer restore,
  and active-writer refusal.
- A Windows live E2E resumes an affected persisted thread, replaces the App
  Server generation, and archives it successfully through the real protocol.
