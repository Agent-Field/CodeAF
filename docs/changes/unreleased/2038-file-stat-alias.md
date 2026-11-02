---
kind: fixed
title: A desktop file chip in the chat's folder is not called outside through a linked path
pr: 2038
surface: [engine]
invalidates:
  - "A file inside the chat's workspace read as outside this workspace when the file and the workspace were spelled through different links, such as /private/tmp/demo and /tmp/demo on macOS. Containment now also compares resolved paths."
---

A workspace typed as `/tmp/demo` (or through a linked project folder) and a
file a picker or a drag names `/private/tmp/demo/notes.md` are one place. The
desktop's attachment chips and `files.stat` now answer `exists`, `changed` or
`missing` for it instead of `outside`. A path inside the workspace by spelling
keeps the answer it had, and a file that is really elsewhere is still outside.
