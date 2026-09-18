---
kind: fixed
title: the outbox file is compacted back to the rows it still holds
pr: 1133
surface: [engine]
invalidates:
  - "The outbox file at `<profile>/pool/outbox.jsonl` was append-only with nothing that ever rewrote it, so it carried a line for every row an install ever judged — the row, and later the marker that retired it — and grew without bound. Every `Send` and every `Pending` read the whole file back, so a long-lived install's work per judgement grew with the total it had ever judged. A `Send` now rewrites the file to exactly the rows still pending when it has grown mostly into retired ones — more lines than twice the pending rows, past a floor of 1024 lines — through a temporary file fsynced and renamed over the original, and the markers of the rows that are gone go with them. A crash before the rename leaves the old file whole and one after it leaves a file holding the same pending rows, so no row is lost and no retired row comes back to be sent twice. A second codeaf on the same profile that already holds the file keeps writing to the inode the rename unlinked and those appends are lost — the one-writer-per-profile assumption the file already rested on, now made explicit."
---

The file is still append-only between compactions, which is what keeps a crash
mid-write from losing or double-sending a row; the rewrite happens only where
the file's own size is the cost, at the start of a `Send`, before its first POST
and under the same lock that serialises Sends. What stays is only the pending
rows, because a marker means nothing once the row it retired is gone.
