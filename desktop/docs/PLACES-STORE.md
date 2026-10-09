# Places store (`internal/placegraph`)

The canonical, persisted place graph. It is distinct from `session.Place`, which is a session's on-disk folder layout. Exact API: the exported identifiers in `internal/placegraph/{types,graph,membership,store}.go`.

## What it owns

Places (id, name, ordered parents, tint, archive state, context, policy, reserved `manager`), memberships (`chatId`, `placeId`, `addedBy`, `at`), the rail's pinned order, a structural revision, and an in-memory undo ring of 20 receipts. It does not resolve a chat's context, call a model, read sessions or serve HTTP; the bridge and later hook lanes sit on top of `Store.Snapshot()`.

## Storage

One JSON file at a path the caller injects, plus `<path>.lock`. Every call takes the exclusive file lock (`internal/filelock`, 10 s default, then `ErrLocked`), re-reads the file, validates the result of the mutation, and replaces the file by temp + fsync + rename. Two processes on one path never lose an update.

A damaged file is never deleted. Unparseable or structurally invalid (cycle, duplicate id, bad field) input is renamed to `<path>.corrupt-<time>-<id>` and the store starts empty (`Store.LastRecovery()` says so). Dangling references are dropped and listed, with a verbatim copy kept beside. A file from a newer schema version is refused and left alone.

## Rules worth knowing

- Many parents, no cycles. The first parent decides an inherited tint; an explicit tint overrides; a top-level place with none picks the least-used of five.
- Delete and merge move children up to the removed place's parents and keep the colour they showed. Chats are only unfiled, never deleted.
- Archived places keep their memberships and are hidden everywhere; a chat whose places are all archived reads as unplaced.
- Sibling names are unique, case-insensitively.
- Undo needs the graph to be exactly as the receipt's commit left it (`ErrRevisionConflict` otherwise). Place visits (`TouchOpened`) do not move the revision.

## Bounds

`MaxPlaces` 2000, `MaxMemberships` 50000, `MaxParents` 16, `MaxPinned` 50, `MaxChatPlaces` 64, `MaxNameRunes` 120, `MaxInstructions` 64 KiB, `MaxSources` 200, `MaxFileBytes` 32 MiB. Changing one changes this list in the same commit.
