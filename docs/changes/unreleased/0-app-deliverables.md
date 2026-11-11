---
kind: added
title: codeaf app lists, reads, keeps and removes deliverables; task branches and a process URL
pr: 0
surface: [engine]
invalidates:
  - "A deliverable was an untyped index row only the media tools and /export wrote, so an app could not tell a file the agent created from one it edited, could not group files by the message that asked for them, and had no way to take one out of the conversation's folder. The artifacts index row now carries an optional typed `deliver` fact (id, authored message `turn`, producing `run`, `task`, `from`, recorded `state`, `profile`, origin, file identity, kept destination), appended as full-snapshot updates for Keep and Remove. A row without it is a legacy row: its turn, task, inputs and state are unknown and are never inferred from a title, the current turn, a time, or where the file sits today."
  - "The `write` tool wrote a file and nothing else. A NEW .md/.mmd/image/audio/video file under the workspace, work/ or artifacts/ is now a deliverable (kept from the start when it is created inside a borrowed repository, a draft in the session's own folder); an edit to a file that existed, a log, and a file of another kind record nothing. The five media tools add the same fact to the row they already wrote, and stamp the existing `media.message` with the authored message the call was made under (it was never set before). A file created by `bash` is NOT recorded: no seam observes it."
  - "A task worker's files reached no index. A new document a worker writes with the `write` tool is now recorded under the conversation that owns the task (its `task`, the asking message as `turn` only when the task came from one, the worker's own `run`): in a worktree with `branch` and `rel` and state unknown; where the worker stood in the checkout, kept (`created_in_repo`) only when git lists the file untracked and not ignored in a real repository, and unknown in a folder that is no repository. A merged landing settles a row as kept only when HEAD holds exactly the authored bytes at that path, the commit the task was cut from held nothing there, and the checkout file (no link) is that blob; anything less stays unknown, within one 10 s bound. Until then a person's own file at the path the worker's file will have never stands in for it: unmerged, the row is `on_branch` and read from its pinned blob; otherwise the checkout file counts only when it hashes to the authored sha256, and get, read and Keep refuse anything else."
  - "`deliverables.list/count/get/read/keep/remove/snapshot` and `branches.list` are new optional methods; an engine without them answers unknown_method. List and count walk the WHOLE index by cursor (not the newest 2000) with honest `next`/`incomplete`. Keep takes a destination folder from the native host, never overwrites (atomic exclusive link), streams at most 512 MiB, revalidates the source, and writes its index row only after a complete copy. Remove appends a row and never touches the file. Snapshot registers a PNG the desktop already saved; the engine captures nothing."
  - "A background process's first local address was kept as the first regex hit anywhere in its output (`localhost:4321`, any digits as a port, `xlocalhost:3000` and `evil.com/localhost:3000` matching) and never reached the wire. It is now the first http `localhost`/`127.0.0.1` address with a boundary on both sides and a port of 1-65535, kept as an origin (`http://localhost:3000`) and sent as `process.url`."
  - "The app's error codes were a closed set with no word for \"this may have happened\". `uncertain` is added: a Keep whose copy was published but whose record could not be confirmed answers it (no path, no detail) instead of `engine`, so an app re-reads rather than retrying into a second copy."
---

New optional wire: `deliverables.list`, `deliverables.count`, `deliverables.get`,
`deliverables.read`, `deliverables.keep`, `deliverables.remove`,
`deliverables.snapshot`, `branches.list`, the `deliverable.changed` event
(created, kept, removed, snapshot) and `process.url`. See
`docs/design/app-v3/PROTOCOL.md`.

Not built here and stated so: version history ("Since v1"), the temporary-folder
banner's data, and the files shells create. Branch merging is the desktop's.
