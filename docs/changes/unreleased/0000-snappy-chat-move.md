---
kind: changed
title: Continuing a chat on another computer opens the transcript first and fetches files in the background
pr: 0
surface: [chat, engine, remote, app, docs]
invalidates:
  - "Continuing a chat on another computer waited for every workspace file, the previous holder's release, and post-handoff filing before showing the conversation or accepting typing. Continuing now fetches and installs `.cell/` first and opens the chat with the input box active while the remaining workspace files arrive in the background (`Files arriving in the background...`)."
  - "The chat manual stated that the conversation opened only after `copying to this computer` finished. The chat now opens as soon as `.cell/` is installed, and file, search, edit, shell, and git tools wait behind a per-chat workspace-ready gate until the workspace files finish arriving."
  - "`cellsync.Fetcher` fetched missing objects sequentially in batches of 64 with no per-batch retry, and `handoff.Taker.Take` re-read the directory record even when `TakeSeen` already held it. `Fetcher` now runs up to 8 concurrent batch requests (or up to 16 single-object requests) with per-batch retry on transient errors while keeping already-stored objects on disk, and `TakeSeen` passes its seen record to `Taker.Take`."
---

Continuing a chat on another computer now splits the critical path from background work:

- `StageChat` fetches the manifest, tree pages, and `.cell/` objects first, stages and swaps `.cell/` into place, acquires the lease, and fires `OnShown` so the transcript is on screen and the composer accepts typing in at most three serial relay round trips after the holder has released.
- Remaining workspace files materialize in the background while the footer shows `Files arriving in the background (X MB / Y MB)` (or `Files arriving in the background...` when the byte total is not yet known) and clears when done.
- Conversational turns run immediately against the arrived transcript. File, search, edit, shell, and git tools (`read`, `write`, `edit`, `multiedit`, `apply_patch`, `glob`, `grep`, `ls`, `bash`, and `!` user shell commands) wait on `cell.WorkspaceArrival` with `Waiting for workspace files to finish arriving...` and never run against a partial workspace tree.
- Transient relay or storage errors (`429`, `502`, `503`, `504`, `5xx`, timeouts, and connection resets) retry up to 3 times with backoff (`250ms`, `750ms`, `2s` plus jitter) per batch without discarding already-fetched frames. If the background workspace fetch still fails, the chat stays open, the status line shows `Workspace files could not finish arriving — press r to retry (reason)`, and workspace tools refuse with that reason until retry succeeds.
- `CODEAF_MOVE_FILES_FIRST=1` restores the previous files-before-open order for troubleshooting.
