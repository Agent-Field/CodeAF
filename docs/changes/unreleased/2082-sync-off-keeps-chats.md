---
kind: fixed
title: Sync off keeps every chat listed and writable, and a copied older chat can no longer be wiped
pr: 2082
surface: [chat, remote, docs]
invalidates:
  - "With CODEAF_SYNC_URL=off or sync quiet before pairing, movablePlace returned before looking for a completed .moving copy, so the untouched original opened; now an existing copy opens whatever sync says, and only preparing a new copy needs sync."
  - "A rewind or a take onto a .moving copy removed .cell/copy-ready, and opening the original afterwards ran CopyLegacy, which removed the copy and every turn since; now restores keep this computer's .cell bookkeeping (copy-ready, copy-offered, published.json, adopted.json are in chainNames), a take re-stamps the copy markers, and CopyLegacy never removes a destination holding sealed turns."
  - "Each reader of a project bucket walked its folders and resolved cell.Copied on its own; now session.BucketSessionDirs is the one walk over a bucket and its .moving folder, reading .moving at most once and making no copy lookups when a bucket has none."
  - "#2072's two doctor items (naming a sync service capability gap, naming a removed device still referenced locally) are not built: the capability header only comes back on a write, and local references store display labels that change on a rename."
---
